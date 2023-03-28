package worker

import (
	"context"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	dockerClient "github.com/docker/docker/client"
	"log"
	"strings"
	"time"
)

type containerInfo struct {
	Id              string
	Name            string
	NetworkId       []string
	VoyagerSettings voyagerSettings
}

type voyagerSettings struct {
	DomainName     []string
	DomainTls      bool
	ServiceIp      string
	ServicePort    string
	ServiceTls     bool
	ServiceHeaders map[string][]string
}

type Worker struct {
	ctx    context.Context
	client dockerClient.Client

	traefikContainerId string
	traefikConfigFile  string
	containerInfo      map[string]containerInfo // map of container id => info
	networkInfo        map[string][]string      // map of network id => list of container ids

	cancel func()
}

func New(containerId string, traefikConfigFile string, ctx context.Context, cancel func()) (*Worker, error) {
	return &Worker{
		ctx:                ctx,
		cancel:             cancel,
		traefikContainerId: containerId,
		traefikConfigFile:  traefikConfigFile,
		containerInfo:      make(map[string]containerInfo),
		networkInfo:        make(map[string][]string),
	}, nil
}

func (w *Worker) Start() {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				log.Print(err)
			}
		}()

		client, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
		defer func() {
			_ = client.Close()
		}()
		if err != nil {
			panic(err)
		}
		w.client = *client
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	w.writeTraefikConfig()

	eventFilters := filters.NewArgs()
	eventFilters.Add("type", "container")
	eventFilters.Add("label", "voyager.domain.name")
	eventFilters.Add("event", "start")
	eventFilters.Add("event", "destroy")
	dockerMessages, dockerErrors := w.client.Events(w.ctx, types.EventsOptions{Filters: eventFilters})

	for {
		select {
		case err := <-dockerErrors:
			panic(err)
		case msg := <-dockerMessages:
			w.updateContainerInfo(msg.Actor.ID, msg.Action == "destroy")
			w.updateNetworkInfo()
			w.updateContainerNetworks()
			w.writeTraefikConfig()
		case <-ticker.C:
			w.tickerDoWork()
			w.updateNetworkInfo()
			w.updateContainerNetworks()
			w.writeTraefikConfig()
		case <-w.ctx.Done():
			return
		}
	}
}

func (w *Worker) Stop() error {
	w.containerInfo = map[string]containerInfo{}
	w.updateNetworkInfo()
	w.updateContainerNetworks()
	w.cancel()
	return nil
}

func (w *Worker) tickerDoWork() {
	defer w.writeTraefikConfig()
	containerFilters := filters.NewArgs()
	containerFilters.Add("label", "voyager.domain.name")
	containers, err := w.client.ContainerList(context.Background(), types.ContainerListOptions{All: true, Filters: containerFilters})
	if err != nil {
		panic(err)
	}

	for _, container := range containers {
		w.updateContainerInfo(container.ID, false)
	}
}

func (w *Worker) updateContainerInfo(containerId string, removed bool) {
	if removed {
		delete(w.containerInfo, containerId)
		return
	}
	containerInfoJson, err := w.client.ContainerInspect(w.ctx, containerId)
	if err != nil {
		panic(err)
	}

	networkIdList := make([]string, 0)
	for _, item := range containerInfoJson.NetworkSettings.Networks {
		networkIdList = append(networkIdList, item.NetworkID)
	}
	servicePort := "80"
	for k := range containerInfoJson.Config.ExposedPorts {
		if !strings.HasSuffix(string(k), "/tcp") {
			continue
		}
		servicePort = strings.TrimSuffix(string(k), "/tcp")
	}
	if val, ok := containerInfoJson.Config.Labels["voyager.service.port"]; ok {
		servicePort = strings.TrimSuffix(val, "/tcp")
	}
	serviceHeaders := map[string][]string{}
	w.containerInfo[containerInfoJson.ID] = containerInfo{
		Id:        containerInfoJson.ID,
		Name:      containerInfoJson.Name,
		NetworkId: networkIdList,
		VoyagerSettings: voyagerSettings{
			DomainName:     strings.Split(containerInfoJson.Config.Labels["voyager.domain.name"], ","),
			DomainTls:      containerInfoJson.Config.Labels["voyager.domain.tls"] == "true",
			ServiceIp:      containerInfoJson.NetworkSettings.IPAddress,
			ServicePort:    servicePort,
			ServiceTls:     containerInfoJson.Config.Labels["voyager.service.tls"] == "true",
			ServiceHeaders: serviceHeaders,
		},
	}
}

func (w *Worker) updateNetworkInfo() {
	w.networkInfo = map[string][]string{}

	for _, containerInfo := range w.containerInfo {
		for _, networkId := range containerInfo.NetworkId {
			if _, found := w.networkInfo[networkId]; found == false {
				w.networkInfo[networkId] = make([]string, 0)
			}
			w.networkInfo[networkId] = append(w.networkInfo[networkId], containerInfo.Id)
		}
	}
}

func (w *Worker) updateContainerNetworks() {
	defer func() {
		if err := recover(); err != nil {
			log.Print(err)
		}
	}()
	if w.traefikContainerId == "" {
		return
	}

	containerInfoJson, err := w.client.ContainerInspect(w.ctx, w.traefikContainerId)
	if err != nil {
		panic(err)
	}

OuterRemove:
	for _, existingNetwork := range containerInfoJson.NetworkSettings.Networks {
		if existingNetwork.EndpointID == containerInfoJson.NetworkSettings.EndpointID {
			continue
		}
		for expectedNetworkId := range w.networkInfo {
			if existingNetwork.NetworkID == expectedNetworkId {
				continue OuterRemove
			}
		}

		err := w.client.NetworkDisconnect(w.ctx, existingNetwork.NetworkID, containerInfoJson.ID, true)
		if err != nil {
			panic(err)
		}
	}

OuterAdd:
	for expectedNetworkId := range w.networkInfo {
		for _, existingNetwork := range containerInfoJson.NetworkSettings.Networks {
			if existingNetwork.NetworkID == expectedNetworkId {
				continue OuterAdd
			}
		}

		err := w.client.NetworkConnect(w.ctx, expectedNetworkId, containerInfoJson.ID, of(network.EndpointSettings{}))
		if err != nil {
			panic(err)
		}
	}
}
