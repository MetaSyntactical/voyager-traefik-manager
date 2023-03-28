package worker

import (
	"fmt"
	"github.com/ghodss/yaml"
	"github.com/traefik/genconf/dynamic"
	"github.com/traefik/genconf/dynamic/types"
	"os"
	"reflect"
	"regexp"
	"strings"
)

func (w *Worker) writeTraefikConfig() {
	config := dynamic.Configuration{
		HTTP: &dynamic.HTTPConfiguration{
			Middlewares: map[string]*dynamic.Middleware{
				"web-to-websecure": {
					RedirectScheme: of(dynamic.RedirectScheme{
						Scheme: "https",
					}),
				},
			},
			Routers: map[string]*dynamic.Router{
				"catchall-web": {
					EntryPoints: []string{"web"},
					Rule:        "PathPrefix(`/`)",
					Service:     "unavailable@file",
					Priority:    1,
				},
				"catchall-websecure": {
					EntryPoints: []string{"websecure"},
					Rule:        "PathPrefix(`/`)",
					Service:     "unavailable@file",
					Priority:    1,
				},
			},
			Services: map[string]*dynamic.Service{
				"unavailable": {
					LoadBalancer: &dynamic.ServersLoadBalancer{
						PassHostHeader: of(true),
						Servers: []dynamic.Server{
							{URL: "http://127.0.0.1:8000/"},
						},
					},
				},
			},
		},
	}

	for _, container := range w.containerInfo {
		serviceUrl := "http://%s:%s/"
		if container.VoyagerSettings.ServiceTls {
			serviceUrl = "https://%s:%s/"
		}
		addService(config.HTTP, container.Id, of(dynamic.Service{
			LoadBalancer: &dynamic.ServersLoadBalancer{
				PassHostHeader: of(true),
				Servers: []dynamic.Server{
					{URL: fmt.Sprintf(serviceUrl, container.VoyagerSettings.ServiceIp, container.VoyagerSettings.ServicePort)},
				},
			},
		}))
		// TODO: Check if TLS for domain is enabled .. if yes, redirect non-tls to tls; if no use below
		var hostRule []string
		for _, domainName := range container.VoyagerSettings.DomainName {
			hostRule = append(hostRule, fmt.Sprintf("HostRegexp(`^%s$`)", convertDomainNameToTraefikRegexp(domainName)))
		}
		if container.VoyagerSettings.DomainTls {
			addRouter(config.HTTP, fmt.Sprintf("%s-web", container.Id), of(dynamic.Router{
				EntryPoints: []string{"web"},
				Rule:        strings.Join(hostRule[:], "||"),
				Service:     "noop@internal",
				Middlewares: []string{"web-to-websecure"},
			}))
			addRouter(config.HTTP, fmt.Sprintf("%s-websecure", container.Id), of(dynamic.Router{
				EntryPoints: []string{"websecure"},
				Rule:        strings.Join(hostRule[:], "||"),
				Service:     fmt.Sprintf("%s@file", container.Id),
				TLS: of(dynamic.RouterTLSConfig{
					Domains: []types.Domain{
						{Main: container.VoyagerSettings.DomainName[0], SANs: container.VoyagerSettings.DomainName[1:]},
					},
				}),
			}))
		} else {
			addRouter(config.HTTP, fmt.Sprintf("%s-web", container.Id), of(dynamic.Router{
				EntryPoints: []string{"web"},
				Rule:        strings.Join(hostRule[:], "||"),
				Service:     fmt.Sprintf("%s@file", container.Id),
			}))
		}
	}

	b, _ := yaml.Marshal(config)

	if w.traefikConfigFile == "" {
		fmt.Printf("%s\n", b)
	} else {
		err := os.WriteFile(w.traefikConfigFile, b, 0644)
		if err != nil {
			panic(err)
		}
	}
}

func of[Value any](v Value) *Value {
	return &v
}

func convertDomainNameToTraefikRegexp(domainName string) string {
	var reDots = regexp.MustCompile(`(\.)`)
	domainName = reDots.ReplaceAllString(domainName, `\.`)
	var reAsterisk = regexp.MustCompile(`(\*)`)
	domainName = reAsterisk.ReplaceAllString(domainName, `.+`)
	return domainName
}

func addService(configuration *dynamic.HTTPConfiguration, serviceName string, service *dynamic.Service) bool {
	if _, ok := configuration.Services[serviceName]; !ok {
		configuration.Services[serviceName] = service
		return true
	}

	uniq := map[string]struct{}{}
	for _, server := range configuration.Services[serviceName].LoadBalancer.Servers {
		uniq[server.URL] = struct{}{}
	}

	for _, server := range service.LoadBalancer.Servers {
		if _, ok := uniq[server.URL]; !ok {
			configuration.Services[serviceName].LoadBalancer.Servers = append(configuration.Services[serviceName].LoadBalancer.Servers, server)
		}
	}

	return true
}

func addRouter(configuration *dynamic.HTTPConfiguration, routerName string, router *dynamic.Router) bool {
	if _, ok := configuration.Routers[routerName]; !ok {
		configuration.Routers[routerName] = router
		return true
	}

	return reflect.DeepEqual(configuration.Routers[routerName], router)
}
