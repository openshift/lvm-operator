package main

import (
	_ "embed"
	"reflect"
	"testing"
)

//go:embed testdata/parsed.json
var parsed string

func Test_parseMetrics(t *testing.T) {
	type args struct {
		metricsResult string
	}
	tests := []struct {
		name    string
		args    args
		want    []RawMetric
		wantErr bool
	}{

		{
			"test default",
			args{parsed},
			[]RawMetric{
				{
					Pod:       "vg-manager-x8r2r",
					Container: "vg-manager",
					Value:     0.008179542666666668,
				},
				{
					Pod:       "topolvm-controller-5dd6c8bd59-dlzfw",
					Container: "csi-provisioner",
					Value:     0.0015656359999999992,
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMetrics(tt.args.metricsResult)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseMetrics() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseMetrics() got = %v, want %v", got, tt.want)
			}
		})
	}
}
