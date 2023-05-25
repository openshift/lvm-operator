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

package controllers

const (
	TopolvmCSIDriverName = "topolvm.io"

	VGManagerServiceAccount = "vg-manager"

	TopolvmControllerServiceAccount = "topolvm-controller"

	// CSI Controller container and deployment names
	TopolvmControllerDeploymentName = "topolvm-controller"
	TopolvmControllerContainerName  = "topolvm-controller"
	CsiResizerContainerName         = "csi-resizer"
	CsiProvisionerContainerName     = "csi-provisioner"
	CsiSnapshotterContainerName     = "csi-snapshotter"
	CsiLivenessProbeContainerName   = "liveness-probe"

	// CSI Controller health endpoints
	TopolvmControllerContainerHealthzName   = "healthz"
	TopolvmControllerContainerLivenessPort  = int32(9808)
	TopolvmControllerContainerReadinessPort = int32(8080)

	// CSI Controller resource requests
	TopolvmControllerMemRequest = "45Mi"
	TopolvmControllerCPURequest = "2m"

	TopolvmCsiProvisionerMemRequest = "50Mi"
	TopolvmCsiProvisionerCPURequest = "2m"

	TopolvmCsiResizerMemRequest = "35Mi"
	TopolvmCsiResizerCPURequest = "1m"

	TopolvmCsiSnapshotterMemRequest = "35Mi"
	TopolvmCsiSnapshotterCPURequest = "1m"

	VgManagerMemRequest = "45Mi"
	VgManagerCPURequest = "2m"

	CertGeneratorMemRequest = "15Mi"
	CertGeneratorCPURequest = "1m"

	// topoLVM Node resource requests
	TopolvmNodeMemRequest = "25Mi"
	TopolvmNodeCPURequest = "1m"

	TopolvmdMemRequest = "30Mi"
	TopolvmdCPURequest = "2m"

	CSIRegistrarMemRequest = "15Mi"
	CSIRegistrarCPURequest = "1m"

	LivenessProbeMemRequest = "15Mi"
	LivenessProbeCPURequest = "1m"

	FileCheckerMemRequest = "10Mi"
	FileCheckerCPURequest = "1m"

	// CSI Provisioner requires below environment values to make use of CSIStorageCapacity
	PodNameEnv   = "POD_NAME"
	NameSpaceEnv = "NAMESPACE"

	// topoLVM Node
	TopolvmNodeServiceAccount       = "topolvm-node"
	TopolvmNodeDaemonsetName        = "topolvm-node"
	CSIKubeletRootDir               = "/var/lib/kubelet/"
	NodeContainerName               = "topolvm-node"
	TopolvmNodeContainerHealthzName = "healthz"
	LvmdConfigFile                  = "/etc/topolvm/lvmd.yaml"

	DefaultCSISocket  = "/run/topolvm/csi-topolvm.sock"
	DefaultLVMdSocket = "/run/lvmd/lvmd.sock"
	DeviceClassKey    = "topolvm.io/device-class"

	// default fstype for topolvm storage classes
	TopolvmFilesystemType = "xfs"

	// name of the lvm-operator container
	LVMOperatorContainerName = "manager"

	// annotations

	// WorkloadPartitioningManagement contains the management workload annotation
	workloadPartitioningManagementAnnotation = "target.workload.openshift.io/management"

	managementAnnotationVal = `{"effect": "PreferredDuringScheduling"}`

	// labels and values

	// AppKubernetesPartOfLabel is the Kubernetes recommended part-of label
	AppKubernetesPartOfLabel = "app.kubernetes.io/part-of"
	// AppKubernetesNameLabel is the Kubernetes recommended name label
	AppKubernetesNameLabel = "app.kubernetes.io/name"
	// AppKubernetesManagedByLabel is the Kubernetes recommended managed-by label
	AppKubernetesManagedByLabel = "app.kubernetes.io/managed-by"
	// AppKubernetesComponentLabel is the Kubernetes recommended component label
	AppKubernetesComponentLabel = "app.kubernetes.io/component"

	TopolvmControllerLabelVal = "topolvm-controller"
	TopolvmNodeLabelVal       = "topolvm-node"
	VGManagerLabelVal         = "vg-manager"
	ManagedByLabelVal         = "lvms-operator"
	PartOfLabelVal            = "lvms-provisioner"
	CsiDriverNameVal          = "topolvm-csi-driver"

	storageClassPrefix        = "lvms-"
	volumeSnapshotClassPrefix = "lvms-"
	sccPrefix                 = "lvms-"
)
