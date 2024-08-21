package main

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// parseMetrics parse the values from a metrics result
func parseMetrics(metricsResult string) ([]RawMetric, error) {
	type subject struct {
		Pod       string
		Container string
	}
	type series struct {
		Metric subject
		Value  []interface{}
	}
	type resultData struct {
		ResultType string
		Result     []series
	}
	type metricsData struct {
		Status string
		Data   resultData
	}

	var d metricsData
	err := json.Unmarshal([]byte(metricsResult), &d)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics: %w", err)
	}

	metrics := make([]RawMetric, 0)

	for _, res := range d.Data.Result {
		if res.Metric.Container == "POD" {
			continue
		}
		v, err := strconv.ParseFloat(fmt.Sprintf("%s", res.Value[1]), 64)
		if err != nil {
			return nil, fmt.Errorf("error converting %s to number", res.Value[1])
		}
		metrics = append(metrics, RawMetric{
			Pod:       res.Metric.Pod,
			Container: res.Metric.Container,
			Value:     v,
		})
	}

	return metrics, nil
}
