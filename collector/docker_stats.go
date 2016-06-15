// Copyright 2015 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// +build !nodocker

package collector

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"sync"

	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"golang.org/x/net/context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

var (
	dockerAddr     = flag.String("collector.docker.addr", "unix:///var/run/docker.sock", "The location of the docker daemon socket or endpoint")
	defaultHeaders = map[string]string{"User-Agent": "engine-api-cli-1.0"}
)

// DockerCollector orchestrates the collectors for Docker containers
type DockerCollector struct {
	mtx        sync.RWMutex
	containers []types.Container
}

func init() {
	Factories["docker"] = NewDockerCollector
}

// NewDockerCollector instanstiates DockerCollector
func NewDockerCollector() (Collector, error) {
	return &DockerCollector{
		mtx: sync.RWMutex{},
	}, nil
}

// Update - checks for new/departed containers and scrapes them
func (c *DockerCollector) Update(ch chan<- prometheus.Metric) (err error) {

	c.updateContainerList()

	var wg sync.WaitGroup
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	for _, c := range c.containers {
		wg.Add(1)
		go func(container types.Container) {
			defer wg.Done()
			s := scrapeDockerStats(container)
			// set container name
			var labels = make(prometheus.Labels)
			labels["name"] = strings.TrimPrefix(container.Names[0], "/")
			for lk, lv := range container.Labels {
				labels[lk] = lv
			}
			// build new cpu counter metric
			m := prometheus.NewCounter(prometheus.CounterOpts{
				Namespace:   Namespace,
				Subsystem:   "docker",
				Name:        string("cpu_total_usage"),
				Help:        fmt.Sprintf("CPU total usage in nanoseconds"),
				ConstLabels: labels,
			})
			//set and collect
			m.Set(float64(s.CPUStats.CPUUsage.TotalUsage))
			m.Collect(ch)

			// build new cpu counter metric
			m = prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace:   Namespace,
				Subsystem:   "docker",
				Name:        string("memory_usage"),
				Help:        fmt.Sprintf("Memory usage in bytes"),
				ConstLabels: labels,
			})
			//set and collect
			m.Set(float64(s.MemoryStats.Usage))
			m.Collect(ch)

			// build new cpu counter metric
			m = prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace:   Namespace,
				Subsystem:   "docker",
				Name:        string("memory_limit"),
				Help:        fmt.Sprintf("Memory limit in bytes"),
				ConstLabels: labels,
			})
			//set and collect
			m.Set(float64(s.MemoryStats.Limit))
			m.Collect(ch)
		}(c)
	}

	wg.Wait()
	return nil
}

var dc *client.Client

func getDockerClient() (dockerClient *client.Client, err error) {
	if dc == nil {
		log.Debugf("Creating new Docker api client")
		dockerClient, err = client.NewClient(*dockerAddr, "v1.22", nil, defaultHeaders)
		dc = dockerClient
	}
	return dc, err
}

func (c *DockerCollector) updateContainerList() {
	// Update - checks for new/departed containers and scrapes them
	log.Debugf("Fetching list of locally running containers")
	cli, err := getDockerClient()
	if err != nil {
		log.Errorf("Failed to create Docker api client: %s", err.Error())
		return
	}

	options := types.ContainerListOptions{All: false, Quiet: true}
	containers, err := cli.ContainerList(context.Background(), options)
	if err != nil {
		log.Errorf("Failed to fetch container list: %s", err.Error())
	}

	var ids []types.Container
	for _, c := range containers {
		log.Debugf("found %s", c.Names[0])
		ids = append(ids, c)
	}
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.containers = ids
}

func scrapeDockerStats(container types.Container) *types.Stats {
	log.Debugf("Scraping container stats for %s", container.Names[0])
	cli, err := getDockerClient()
	if err != nil {
		log.Errorf("Failed to create Docker api client: %s", err.Error())
		return nil
	}
	rc, err := cli.ContainerStats(context.Background(), container.ID, false)
	if err != nil {
		log.Errorf("Failed to fetch docker container stats for %s: %s", container.Names[0], err.Error())
		return nil
	}
	defer rc.Close()
	decoder := json.NewDecoder(rc)
	var stats types.Stats
	err = decoder.Decode(&stats)
	if err != nil {
		log.Errorf("Couldn't decode stats json from container %s: %s", container.Names[0], err.Error())
		return nil
	}

	return &stats
}
