package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"
	"log"
	"time"
)

type containerInfo struct {
	Id              string
	Name            string
	NetworkId       []string
	VoyagerSettings voyagerSettings
}

type voyagerSettings struct {
	DomainName     string
	DomainTls      bool
	ServicePort    int
	ServiceTls     bool
	ServiceHeaders map[string][]string
}

type Worker struct {
	ctx           context.Context
	client        dockerClient.Client
	containerInfo map[string]containerInfo // map of container id => info
	networkInfo   map[string][]string      // map of network id => list of container ids

	cancel func()
}

func New(ctx context.Context, cancel func()) (*Worker, error) {
	return &Worker{
		ctx:           ctx,
		cancel:        cancel,
		containerInfo: make(map[string]containerInfo),
		networkInfo:   make(map[string][]string),
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
			w.writeTraefikConfig()
		case <-ticker.C:
			// TODO: Enable ticker
			// w.tickerDoWork()
		case <-w.ctx.Done():
			return
		}
	}
}

func (w *Worker) Stop() error {
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
	defer w.updateNetworkInfo()

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
	w.containerInfo[containerInfoJson.ID] = containerInfo{
		Id:              containerInfoJson.ID,
		Name:            containerInfoJson.Name,
		NetworkId:       networkIdList,
		VoyagerSettings: voyagerSettings{}, // TODO: Generate VoyagerSettings
	}
	b, _ := json.MarshalIndent(containerInfoJson, "", "  ")
	fmt.Printf("containerInfo: %s\n", b)
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

func (w *Worker) writeTraefikConfig() {
	// TODO: Write traefik file provider dynamic config marshalled as yaml file
	fmt.Printf("containerInfo: %+v\n", w.containerInfo)
	fmt.Printf("networkInfo:   %+v\n", w.networkInfo)
}
