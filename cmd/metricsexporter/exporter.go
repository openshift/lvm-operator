/*
Copyright 2021 Red Hat Openshift Data Foundation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"net"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	exporterNamespace string
	exporterAddress   string
	kubeconfigPath    string
	kubeAPIServerURL  string
)

type lvmCollector struct{}

func (lc *lvmCollector) Describe(c chan<- *prometheus.Desc) {}
func (lc *lvmCollector) Collect(c chan<- prometheus.Metric) {}

func main() {
	lvmExpFlags := flag.NewFlagSet("lvm-metrics-exporter", flag.ExitOnError)
	lvmExpFlags.StringVar(&exporterNamespace, "namespace", "lvm-exporter",
		"set the namespace of the lvm metric exporter")
	lvmExpFlags.StringVar(&exporterAddress, "address", ":23532",
		"address on which the metrics exporter should run")
	lvmExpFlags.StringVar(&kubeconfigPath, "kubeconfig", "", "Path to kubeconfig file")
	lvmExpFlags.StringVar(&kubeAPIServerURL, "apiserver", "", "API server URL")

	zapOptions := &zap.Options{}
	zapOptions.BindFlags(lvmExpFlags)

	_ = lvmExpFlags.Parse(os.Args[1:])

	zapOpts := zap.UseFlagOptions(zapOptions)
	newLogger := zap.New(zapOpts).WithName("lvm-metric-exporter")
	newLogger.Info("Commandline: ", "args", os.Args[1:])
	newLogger.Info("Exporter set values: ", "namespace", exporterNamespace, "address", exporterAddress)

	prometheus.MustRegister(&lvmCollector{})

	// prometheus http handler
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		_, err := rw.Write([]byte(`<html>
             <head><title>LVM Metric Exporter</title></head>
				<body>
					<h1>LVM Metric Exporter</h1>
					<p><a href='/metrics'>Metrics</a></p>
				</body>
             </html>`))
		if err != nil {
			newLogger.Error(err, "error while writing into http.ResponseWriter")
			return
		}
	})

	// prometheus metrics
	_ = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "lvmoptest",
		Help: "this is a test gauge metrics",
	})

	// runtime loop
	newLogger.Info("Starting lvm metric exporter at ", "address", exporterAddress)
	ln, err := net.Listen("tcp", exporterAddress)
	if err != nil {
		newLogger.Error(err, "Error creating the listener")
	}
	err = http.Serve(ln, nil)
	if err != nil {
		newLogger.Error(err, "Error while serving requests")
	}
}
