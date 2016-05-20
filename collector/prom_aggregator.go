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

// +build !nopromagg

package collector

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/model"
)

var (
	scrapeTargetsFile = flag.String("collector.promagg.file", "/etc/node_exporter/promagg_targets.json", "A json file in the format '[ { \"endpoint\": \"http://prom:1234/metrics\", \"labels\": { \"label\" : \"value\" } } ]'")
	acceptHeader      = `application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited;q=0.7,text/plain;version=0.0.4;q=0.3,application/json;schema="prometheus/telemetry";version=0.0.2;q=0.2,*/*;q=0.1`
)

type promAggCollector struct {
	targets scrapeTargets
	client  *http.Client
}

type scrapeTarget struct {
	Endpoint string            `json:"endpoint"`
	Labels   map[string]string `json:"labels"`
}
type scrapeTargets []scrapeTarget

func init() {
	Factories["promagg"] = NewPromAggCollector
}

// NewPromAggCollector todo
func NewPromAggCollector() (Collector, error) {
	return &promAggCollector{
		client: &http.Client{},
	}, nil
}

func (c *promAggCollector) Update(ch chan<- prometheus.Metric) (err error) {
	c.fetchEndpoints()
	for _, t := range c.targets {
		log.Debugf("scraping endpoint %s", t.Endpoint)
		samples, err := c.scrape(t.Endpoint)
		if err == nil {
			for _, s := range samples {
				// extract labels from metric
				var labels = make(prometheus.Labels)
				for lk, lv := range s.Metric {
					if lk != "__name__" {
						labels[string(lk)] = string(lv)
					}
				}
				// extract labels from target config
				for lk, lv := range t.Labels {
					labels[string(lk)] = string(lv)
				}
				// build new untyped metric
				m := prometheus.NewGauge(prometheus.GaugeOpts{
					//Namespace: Namespace,
					//Subsystem: "promagg",
					Name:        string(s.Metric["__name__"]),
					Help:        fmt.Sprintf("aggregated field %s from %s.", s.Metric["__name__"], t.Endpoint),
					ConstLabels: labels,
				})
				//set and collect
				m.Set(float64(s.Value))
				m.Collect(ch)
			}
		} else {
			log.Debugf("error: %s", err.Error())
		}
	}
	return err
}

func (c *promAggCollector) fetchEndpoints() {
	targetFile, err := os.Open(*scrapeTargetsFile)
	if err != nil {
		log.Debugf("error opening targets file: %s", err.Error())
		return
	}

	if err = json.NewDecoder(targetFile).Decode(&c.targets); err != nil {
		log.Debugf("error unmarshalling json: %s", err.Error())
		return
	}
}

func (c *promAggCollector) scrape(url string) (model.Samples, error) {
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
