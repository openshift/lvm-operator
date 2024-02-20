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
	CsiRegistrarContainerName       = "csi-registrar"
	CsiResizerContainerName         = "csi-resizer"
	CsiProvisionerContainerName     = "csi-provisioner"
	CsiSnapshotterContainerName     = "csi-snapshotter"
	CsiLivenessProbeContainerName   = "liveness-probe"

	// CSI Controller health endpoints
	TopolvmControllerContainerHealthzName   = "healthz"
	TopolvmControllerContainerLivenessPort  = int32(9808)
	TopolvmControllerContainerReadinessPort = int32(8080)

	// CSI Controller resource requests/limits
	// TODO: Reduce these values and reach optimistic values without effecting performance
	TopolvmControllerMemRequest = "100Mi"
	TopolvmControllerCPURequest = "100m"

	TopolvmCsiProvisionerMemRequest = "50Mi"
	TopolvmCsiProvisionerCPURequest = "50m"

	TopolvmCsiResizerMemRequest = "50Mi"
	TopolvmCsiResizerCPURequest = "20m"

	TopolvmCsiSnapshotterMemRequest = "50Mi"
	TopolvmCsiSnapshotterCPURequest = "20m"

	VgManagerMemRequest = "50Mi"
	VgManagerCPURequest = "30m"

	// topoLVM Node resource requests/limits
	TopolvmNodeMemRequest = "150Mi"
	TopolvmNodeCPURequest = "50m"

	TopolvmdMemRequest = "100Mi"
	TopolvmdCPURequest = "150m"

	CSIRegistrarMemRequest = "30Mi"
	CSIRegistrarCPURequest = "20m"

	LivenessProbeMemRequest = "30Mi"
	LivenessProbeCPURequest = "30m"

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
