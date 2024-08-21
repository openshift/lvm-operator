package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Collector struct {
	Start time.Time
	End   time.Time

	PrometheusURL string
	Namespace     string

	Total         Container
	AVGPerLVMNode Container
	Pods          map[string]map[string]Container

	token  string
	filter string
	nodes  int
	client *http.Client
}

type Container struct {
	Cpu *Metric
	Mem *Metric
}

type Metric struct {
	Quantile99 float64
	Quantile95 float64
	Quantile90 float64
}

type RawMetric struct {
	Pod       string
	Container string
	Value     float64
}

func NewCollector(url, token, namespace string, start, end time.Time) *Collector {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}
	clnt := &http.Client{Transport: tr}

	return &Collector{
		PrometheusURL: url,
		token:         token,
		Namespace:     namespace,
		Start:         start,
		End:           end,
		client:        clnt,
		Pods:          make(map[string]map[string]Container),
		Total:         Container{Mem: &Metric{}, Cpu: &Metric{}},
		AVGPerLVMNode: Container{Mem: &Metric{}, Cpu: &Metric{}},
	}
}

func (c *Collector) quantileMemory(quantile string) string {
	timeframe := fmt.Sprintf("%.0fs", c.End.Sub(c.Start).Round(time.Second).Seconds())
	return fmt.Sprintf(`quantile_over_time(%s, sum by (pod, container) ( container_memory_working_set_bytes{namespace="%s", container!=""} )[%s:] ) / 1024^2`, quantile, c.Namespace, timeframe)
}
func (c *Collector) quantileCPU(quantile string) string {
	timeframe := fmt.Sprintf("%.0fs", c.End.Sub(c.Start).Round(time.Second).Seconds())
	return fmt.Sprintf(`quantile_over_time(%s,sum by (pod, container) (irate(container_cpu_usage_seconds_total{namespace="%s", image!=""}[%s]))[%s:]) * 1000`, quantile, c.Namespace, timeframe, timeframe)
}

func (c *Collector) collect(ctx context.Context) error {
	for _, entry := range []struct {
		quantile string
		adder    func(*Metric, float64)
	}{
		{"0.95", func(metric *Metric, f float64) {
			metric.Quantile95 += f
		}},
		{"0.99", func(metric *Metric, f float64) {
			metric.Quantile99 += f
		}},
		{"0.90", func(metric *Metric, f float64) {
			metric.Quantile90 += f
		}},
	} {
		cpuRAW, err := c.fetchPrometheusMetrics(ctx, c.quantileCPU(entry.quantile))
		if err != nil {
			return fmt.Errorf("could not collect CPU Metrics: %w", err)
		}
		cpu, err := parseMetrics(cpuRAW)
		if err != nil {
			return fmt.Errorf("could not parse CPU Metrics: %w", err)
		}

		memRAW, err := c.fetchPrometheusMetrics(ctx, c.quantileMemory(entry.quantile))
		if err != nil {
			return fmt.Errorf("could not collect MEM Metrics: %w", err)
		}
		mem, err := parseMetrics(memRAW)
		if err != nil {
			return fmt.Errorf("could not parse MEM Metrics: %w", err)
		}
		for _, cpuEntry := range cpu {
			if c.filter != "" && strings.Contains(cpuEntry.Pod, c.filter) {
				continue
			}

			pod, ok := c.Pods[cpuEntry.Pod]
			if !ok {
				pod = make(map[string]Container)
				c.Pods[cpuEntry.Pod] = pod
			}
			container, ok := pod[cpuEntry.Container]
			if !ok {
				container = Container{Mem: &Metric{}, Cpu: &Metric{}}
				pod[cpuEntry.Container] = container
			}

			entry.adder(container.Cpu, cpuEntry.Value)
			entry.adder(c.Total.Cpu, cpuEntry.Value)
		}

		for _, memEntry := range mem {
			if c.filter != "" && strings.Contains(memEntry.Pod, c.filter) {
				continue
			}

			pod, ok := c.Pods[memEntry.Pod]
			if !ok {
				pod = make(map[string]Container)
				c.Pods[memEntry.Pod] = pod
			}
			container, ok := pod[memEntry.Container]
			if !ok {
				container = Container{Mem: &Metric{}, Cpu: &Metric{}}
				pod[memEntry.Container] = container
			}

			entry.adder(container.Mem, memEntry.Value)
			entry.adder(c.Total.Mem, memEntry.Value)
		}
	}

	if c.nodes > 0 {
		c.AVGPerLVMNode.Mem.Quantile90 = c.Total.Mem.Quantile90 / float64(c.nodes)
		c.AVGPerLVMNode.Mem.Quantile95 = c.Total.Mem.Quantile95 / float64(c.nodes)
		c.AVGPerLVMNode.Mem.Quantile99 = c.Total.Mem.Quantile99 / float64(c.nodes)
		c.AVGPerLVMNode.Cpu.Quantile90 = c.Total.Cpu.Quantile90 / float64(c.nodes)
		c.AVGPerLVMNode.Cpu.Quantile95 = c.Total.Cpu.Quantile95 / float64(c.nodes)
		c.AVGPerLVMNode.Cpu.Quantile99 = c.Total.Cpu.Quantile99 / float64(c.nodes)
	}

	return nil
}

func (c *Collector) serialize(dir string) (*os.File, error) {
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return nil, fmt.Errorf("cannot open working directory: %w", err)
		}
	}

	file, err := os.Create(filepath.Join(dir, fmt.Sprintf("metrics-%v-%v.toml", c.Start.Unix(), c.End.Unix())))
	if err != nil {
		return nil, fmt.Errorf("could not create metrics report file: %w", err)
	} else {
		if err := toml.NewEncoder(file).Encode(c); err != nil {
			return nil, fmt.Errorf("could not write metrics report file: %w", err)
		}
	}
	return file, nil
}

// fetchPrometheusMetrics fetches the metrics from the server
func (c *Collector) fetchPrometheusMetrics(ctx context.Context, query string) (string, error) {
	req, err := c.prometheusRequest(query)
	if err != nil {
		return "", fmt.Errorf("could not setup prometheus request: %w", err)
	}

	req = req.WithContext(ctx)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch metrics from prometheus: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != 200 {
		if resp.StatusCode == 403 {
			return "", fmt.Errorf("probably token expired, renew token and try to execute again providing the new token")
		}
		return "", fmt.Errorf("error during metrics readout: %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading metrics response at %s: %w", req.URL, err)
	}

	return string(data), nil
}

func (c *Collector) prometheusRequest(query string) (*http.Request, error) {
	params := url.Values{}
	params.Add("namespace", c.Namespace)
	params.Add("query", query)
	params.Add("timeout", "30s")
	body := strings.NewReader("")
	metricsQuery := fmt.Sprintf("%s/api/v1/query?%s", c.PrometheusURL, params.Encode())
	req, err := http.NewRequest(http.MethodGet, metricsQuery, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	return req, nil
}

func (c *Collector) SetPodFilter(filter string) {
	c.filter = filter
}

func (c *Collector) SetLVMNodes(nodes int) {
	c.nodes = nodes
}
