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

// +build !nodockagg

package collector

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/model"
)

var (
	exposedMetricsPort = flag.String("collector.dockagg.port", "9090", "The internally exposed, but randomly published, docker port to scrape across all containers")
	acceptHeader       = `application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited;q=0.7,text/plain;version=0.0.4;q=0.3,application/json;schema="prometheus/telemetry";version=0.0.2;q=0.2,*/*;q=0.1`
)

type dockAggCollector struct {
	endpoints []string
	client    *http.Client
}

func init() {
	Factories["dockagg"] = NewDockaggCollector
}

// NewDockaggCollector todo
func NewDockaggCollector() (Collector, error) {
	return &dockAggCollector{
		client: &http.Client{},
	}, nil
}

func (c *dockAggCollector) Update(ch chan<- prometheus.Metric) (err error) {
	log.Debugf("Not implemented")
	c.fetchEndpoints()
	for _, ep := range c.endpoints {
		samples, err := c.scrape(ep)
		if err == nil {
			for _, s := range samples {
				s.Metric.Collect(ch)
			}
		}
	}
	return err
}

func (c *dockAggCollector) fetchEndpoints() {
	c.endpoints = append(c.endpoints, "localhost:9090")
}
func (c *dockAggCollector) scrape(url string) (model.Samples, error) {
	ts := time.Now()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", acceptHeader)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned HTTP status %s", resp.Status)
	}

	var (
		allSamples = make(model.Samples, 0, 200)
		decSamples = make(model.Vector, 0, 50)
	)
	sdec := expfmt.SampleDecoder{
		Dec: expfmt.NewDecoder(resp.Body, expfmt.ResponseFormat(resp.Header)),
		Opts: &expfmt.DecodeOptions{
			Timestamp: model.TimeFromUnixNano(ts.UnixNano()),
		},
	}

	for {
		if err = sdec.Decode(&decSamples); err != nil {
			break
		}
		allSamples = append(allSamples, decSamples...)
		decSamples = decSamples[:0]
	}

	if err == io.EOF {
		// Set err to nil since it is used in the scrape health recording.
		err = nil
	}
	return allSamples, err
}
