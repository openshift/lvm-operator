/*
Copyright 2021.

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

package controllers

import (
	"fmt"
	"os"
)

var (
	defaultValMap = map[string]string{
		"OPERATOR_NAMESPACE":      "openshift-storage",
		"TOPOLVM_CSI_IMAGE":       "quay.io/topolvm/topolvm:0.10.3",
		"CSI_REGISTRAR_IMAGE":     "k8s.gcr.io/sig-storage/csi-node-driver-registrar:v2.3.0",
		"CSI_PROVISIONER_IMAGE":   "k8s.gcr.io/sig-storage/csi-provisioner:v3.0.0",
		"CSI_LIVENESSPROBE_IMAGE": "k8s.gcr.io/sig-storage/livenessprobe:v2.5.0",
		"CSI_RESIZER_IMAGE":       "k8s.gcr.io/sig-storage/csi-resizer:v1.3.0",
		"VGMANAGER_IMAGE":         "vgmanager",
	}
)

var OperatorNamespace string

//CSI
var TopolvmCsiImage string
var CsiRegistrarImage string
var CsiProvisionerImage string
var CsiLivenessProbeImage string
var CsiResizerImage string

var TopolvmCSIDriverName string

var VGManagerImage string

func init() {
	OperatorNamespace = GetEnvOrDefault("OPERATOR_NAMESPACE")

	//CSI
	TopolvmCsiImage = GetEnvOrDefault("TOPOLVM_CSI_IMAGE")
	CsiRegistrarImage = GetEnvOrDefault("CSI_REGISTRAR_IMAGE")
	CsiProvisionerImage = GetEnvOrDefault("CSI_PROVISIONER_IMAGE")
	CsiLivenessProbeImage = GetEnvOrDefault("CSI_LIVENESSPROBE_IMAGE")
	CsiResizerImage = GetEnvOrDefault("CSI_RESIZER_IMAGE")

	TopolvmCSIDriverName = "topolvm.cybozu.com"

	CsiResizerImage = GetEnvOrDefault("CSI_RESIZER_IMAGE")

	VGManagerImage = GetEnvOrDefault("VGMANAGER_IMAGE")
}

func GetEnvOrDefault(env string) string {
	var val string
	val = os.Getenv(env)
	if val == "" {
		val = defaultValMap[env]
	}
	fmt.Printf("%q = %q\n", env, val)
	return val
}
