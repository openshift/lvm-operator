package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
)

func TestCollector(t *testing.T) {
	a := assert.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(parsed))
	}))
	collector := NewCollector(server.URL, "token", "openshift-storage",
		time.Now().Add(-5*time.Minute), time.Now())

	a.NoError(collector.collect(context.Background()))

	file, err := collector.serialize(t.TempDir())
	a.NoError(err)

	file, err = os.Open(file.Name())
	a.NoError(err)

	colFromToml := Collector{}
	a.NoError(toml.NewDecoder(file).Decode(&colFromToml))
	a.Len(colFromToml.Pods, 2)

	a.NotEmpty(colFromToml.PrometheusURL)
}
