/*
Copyright © 2023 Red Hat, Inc.

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

package resource

import (
	"fmt"
	"path/filepath"
	"strings"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/cluster"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/selector"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvmd"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

var (
	hostContainerPropagation      = corev1.MountPropagationHostToContainer
	directoryHostPath             = corev1.HostPathDirectory
	HostPathDirectoryOrCreate     = corev1.HostPathDirectoryOrCreate
	mountPropagationBidirectional = corev1.MountPropagationBidirectional

	devDirPath          = "/dev"
	udevPath            = "/run/udev"
	sysPath             = "/sys"
	metricsCertsDirPath = "/tmp/k8s-metrics-server/serving-certs"
)

var (
	CSIPluginVolName = "csi-plugin-dir"
	CSIPluginVol     = corev1.Volume{
		Name: CSIPluginVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: fmt.Sprintf("%splugins/kubernetes.io/csi", GetAbsoluteKubeletPath(constants.CSIKubeletRootDir)),
				Type: &HostPathDirectoryOrCreate}},
	}
	CSIPluginVolMount = corev1.VolumeMount{
		Name:             CSIPluginVolName,
		MountPath:        fmt.Sprintf("%splugins/kubernetes.io/csi", GetAbsoluteKubeletPath(constants.CSIKubeletRootDir)),
		MountPropagation: &mountPropagationBidirectional,
	}
)

var (
	KubeSANCSIPluginVolName = "kubesan-csi-plugin-dir"
	KubeSANCSIPluginVol     = corev1.Volume{
		Name: KubeSANCSIPluginVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: fmt.Sprintf(
					"%splugins/%s",
					GetAbsoluteKubeletPath(constants.CSIKubeletRootDir),
					constants.KubeSANCSIDriverName,
				),
				Type: &HostPathDirectoryOrCreate}},
	}
	KubeSANCSIPluginLocalVolMount = corev1.VolumeMount{
		Name:             KubeSANCSIPluginVolName,
		MountPath:        fmt.Sprintf("/run/csi"),
		MountPropagation: &mountPropagationBidirectional,
	}
	KubeSANCSIPluginVolMount = corev1.VolumeMount{
		Name: KubeSANCSIPluginVolName,
		MountPath: fmt.Sprintf(
			"%splugins/%s",
			GetAbsoluteKubeletPath(constants.CSIKubeletRootDir),
			constants.KubeSANCSIDriverName,
		),
		MountPropagation: &mountPropagationBidirectional,
	}
)

var (
	TopoLVMNodePluginVolName = "node-plugin-dir"
	TopoLVMNodePluginVol     = corev1.Volume{
		Name: TopoLVMNodePluginVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: fmt.Sprintf("%splugins/%s/node", GetAbsoluteKubeletPath(constants.CSIKubeletRootDir), constants.TopolvmCSIDriverName),
				Type: &HostPathDirectoryOrCreate}},
	}
	TopoLVMNodePluginVolMount = corev1.VolumeMount{
		Name: TopoLVMNodePluginVolName, MountPath: filepath.Dir(constants.DefaultCSISocket),
	}
)

var (
	KubeSANNodeLocalControllerSocketVolName = "kubesan-node-local-controller-socket"
	KubeSANNodeLocalControllerSocketVol     = corev1.Volume{
		Name: KubeSANNodeLocalControllerSocketVolName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium: corev1.StorageMediumMemory,
			},
		},
	}
	KubeSANNodeLocalControllerSocketVolMount = corev1.VolumeMount{
		Name: KubeSANNodeLocalControllerSocketVolName, MountPath: filepath.Dir(constants.ControllerCSILocalPath),
	}
)

var (
	KubeSANNBDPluginVolName = "kubesan-nbd"
	KubeSANNBDPluginVol     = corev1.Volume{
		Name: KubeSANNBDPluginVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: constants.KubeSANNBDDir,
				Type: &HostPathDirectoryOrCreate}},
	}
	KubeSANNBDPluginMount = corev1.VolumeMount{
		Name: KubeSANNBDPluginVolName, MountPath: constants.KubeSANNBDDir,
	}
)

var (
	FileLockVolName = "file-lock-dir"
	FileLockVol     = corev1.Volume{
		Name: FileLockVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: filepath.Dir(util.FileLockDir),
				Type: &HostPathDirectoryOrCreate},
		},
	}
	FileLockVolMount = corev1.VolumeMount{
		Name:      FileLockVolName,
		MountPath: filepath.Dir(util.FileLockDir),
	}
)

var (
	RegistrationVolName = "registration-dir"
	RegistrationVol     = corev1.Volume{
		Name: RegistrationVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: fmt.Sprintf("%splugins_registry/", GetAbsoluteKubeletPath(constants.CSIKubeletRootDir)),
				Type: &HostPathDirectoryOrCreate}},
	}
	RegistrationVolMount = corev1.VolumeMount{
		Name: RegistrationVolName, MountPath: constants.DefaultPluginRegistrationPath,
	}
)

var (
	PodVolName = "pod-volumes-dir"
	PodVol     = corev1.Volume{
		Name: PodVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: fmt.Sprintf("%spods/", GetAbsoluteKubeletPath(constants.CSIKubeletRootDir)),
				Type: &HostPathDirectoryOrCreate}},
	}
	PodVolMount = corev1.VolumeMount{
		Name:             PodVolName,
		MountPath:        fmt.Sprintf("%spods", GetAbsoluteKubeletPath(constants.CSIKubeletRootDir)),
		MountPropagation: &mountPropagationBidirectional,
	}
)

var (
	LVMDConfMapVolName = "lvmd-config"
	LVMDConfMapVol     = corev1.Volume{
		Name: LVMDConfMapVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: filepath.Dir(lvmd.DefaultFileConfigPath),
				Type: &HostPathDirectoryOrCreate},
		},
	}
	LVMDConfMapVolMount = corev1.VolumeMount{
		Name:             LVMDConfMapVolName,
		MountPath:        filepath.Dir(lvmd.DefaultFileConfigPath),
		MountPropagation: &hostContainerPropagation,
	}
)

var (
	DevDirVolName = "device-dir"
	// DevHostDirVol  is the corev1.Volume definition for the "/dev" bind mount used to
	// list block devices.
	// DevMount is the corresponding mount
	DevHostDirVol = corev1.Volume{
		Name: DevDirVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: devDirPath,
				Type: &directoryHostPath,
			},
		},
	}

	// DevHostDirVolMount is the corresponding mount for DevHostDirVol
	DevHostDirVolMount = corev1.VolumeMount{
		Name:             DevDirVolName,
		MountPath:        devDirPath,
		MountPropagation: &hostContainerPropagation,
	}
)

var (
	UdevVolName = "run-udev"
	// UDevHostDirVol is the corev1.Volume definition for the
	// "/run/udev" host bind-mount. This helps lsblk give more accurate output.
	// UDevMount is the corresponding mount
	UDevHostDirVol = corev1.Volume{
		Name: UdevVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: udevPath},
		},
	}
	// UDevHostDirVolMount is the corresponding mount for UDevHostDirVol
	UDevHostDirVolMount = corev1.VolumeMount{
		Name:             UdevVolName,
		MountPath:        udevPath,
		MountPropagation: &hostContainerPropagation,
	}
)

var (
	SysVolName = "sys"
	// SysHostDirVol is the corev1.Volume definition for the
	// "/sys" host bind-mount. This helps discover information about blockd devices
	SysHostDirVol = corev1.Volume{
		Name: SysVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: sysPath},
		},
	}

	// SysHostDirVolMount is the corresponding mount for SysHostDirVol
	SysHostDirVolMount = corev1.VolumeMount{
		Name:             SysVolName,
		MountPath:        sysPath,
		MountPropagation: &hostContainerPropagation,
	}
)

var (
	MetricsCertsVolName = "metrics-cert"
	// MetricsCertsDirVol is the corev1.Volume definition for the
	// certs to be used in metrics endpoint.
	MetricsCertsDirVol = corev1.Volume{
		Name: MetricsCertsVolName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  "vg-manager-metrics-cert",
				DefaultMode: ptr.To[int32](420),
			},
		},
	}
	// MetricsCertsDirVolMount is the corresponding mount for MetricsCertsDirVol
	MetricsCertsDirVolMount = corev1.VolumeMount{
		Name:      MetricsCertsVolName,
		MountPath: metricsCertsDirPath,
		ReadOnly:  true,
	}
)

// templateVGManagerDaemonset returns the desired vgmanager daemonset for a given LVMCluster
func templateVGManagerDaemonset(
	lvmCluster *lvmv1alpha1.LVMCluster,
	clusterType cluster.Type,
	namespace, vgImage string,
	command, args []string,
) appsv1.DaemonSet {
	// aggregate nodeSelector and tolerations from all deviceClasses
	shared, standard := RequiresSharedVolumeGroupSetup(lvmCluster.Spec.Storage.DeviceClasses)

	nodeSelector, tolerations := selector.ExtractNodeSelectorAndTolerations(lvmCluster)
	volumes := []corev1.Volume{
		RegistrationVol,
		FileLockVol,
		PodVol,
		DevHostDirVol,
		UDevHostDirVol,
		SysHostDirVol,
		MetricsCertsDirVol,
		CSIPluginVol,
	}

	if standard {
		confMapVolume := LVMDConfMapVol
		if clusterType == cluster.TypeMicroShift {
			confMapVolume.VolumeSource.HostPath.Path = filepath.Dir(lvmd.MicroShiftFileConfigPath)
		}
		volumes = append(volumes, TopoLVMNodePluginVol, confMapVolume)
	}

	if shared {
		volumes = append(volumes, KubeSANNBDPluginVol, KubeSANCSIPluginVol, KubeSANNodeLocalControllerSocketVol)
	}

	volumeMounts := []corev1.VolumeMount{
		RegistrationVolMount,
		FileLockVolMount,
		PodVolMount,
		DevHostDirVolMount,
		UDevHostDirVolMount,
		SysHostDirVolMount,
		MetricsCertsDirVolMount,
	}

	if standard {
		volumeMounts = append(volumeMounts, TopoLVMNodePluginVolMount, LVMDConfMapVolMount,
			CSIPluginVolMount)
	}

	if shared {
		volumeMounts = append(volumeMounts, KubeSANCSIPluginVolMount, KubeSANNodeLocalControllerSocketVolMount)
	}

	if len(command) == 0 {
		command = []string{"/lvms", "vgmanager"}
	}

	command = append(command, args...)

	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(constants.VgManagerCPURequest),
			corev1.ResourceMemory: resource.MustParse(constants.VgManagerMemRequest),
		},
	}
	containers := []corev1.Container{
		{
			Name:    VGManagerUnit,
			Image:   vgImage,
			Command: command,
			SecurityContext: &corev1.SecurityContext{
				Privileged: ptr.To(true),
				RunAsUser:  ptr.To(int64(0)),
			},
			Ports: []corev1.ContainerPort{
				{Name: constants.TopolvmNodeContainerHealthzName,
					ContainerPort: 8081,
					Protocol:      corev1.ProtocolTCP},
			},
			StartupProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/healthz",
						Port: intstr.FromString(constants.TopolvmNodeContainerHealthzName)}},
				FailureThreshold:    60, // 60*10 = 600s / 10 min for long startup due to large volume group initialization
				InitialDelaySeconds: 2,
				TimeoutSeconds:      2,
				PeriodSeconds:       10},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/healthz",
						Port: intstr.FromString(constants.TopolvmNodeContainerHealthzName)}},
				FailureThreshold:    3,
				InitialDelaySeconds: 1,
				TimeoutSeconds:      1,
				PeriodSeconds:       30},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/readyz",
						Port: intstr.FromString(constants.TopolvmNodeContainerHealthzName)}},
				FailureThreshold:    3,
				InitialDelaySeconds: 1,
				TimeoutSeconds:      1,
				PeriodSeconds:       60},
			VolumeMounts: volumeMounts,
			Resources:    resourceRequirements,
			Env: []corev1.EnvVar{
				{
					Name:  "GOMEMLIMIT",
					Value: fmt.Sprintf("%sB", constants.VgManagerMemRequest),
				},
				{
					Name:  "GOGC",
					Value: "120",
				},
				{
					Name:  "GOMAXPROCS",
					Value: "2",
				},
				{
					Name: "NODE_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "spec.nodeName",
						},
					},
				},
				{
					Name: "NAMESPACE",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.namespace",
						},
					},
				},
				{
					Name: "NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
					},
				},
			},
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		},
	}

	if shared {
		kubeSANNodeVolumeMounts := []corev1.VolumeMount{
			KubeSANCSIPluginLocalVolMount,
			KubeSANNBDPluginMount,
			CSIPluginVolMount,
		}
		containers = append(containers, corev1.Container{
			Name:    "kubesan-csi-node-plugin",
			Image:   constants.KubeSANImage,
			Command: []string{"./kubesan", "csi-node-plugin"},
			SecurityContext: &corev1.SecurityContext{
				Privileged: ptr.To(true),
			},
			VolumeMounts: kubeSANNodeVolumeMounts,
			Env: []corev1.EnvVar{
				{
					Name:  "KUBESAN_IMAGE",
					Value: constants.KubeSANImage,
				},
				{
					Name: "NAMESPACE",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.namespace",
						},
					},
				},
				{
					Name: "NODE_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "spec.nodeName",
						},
					},
				},
			},
		})

		kubeSANControllerVolumeMounts := []corev1.VolumeMount{
			DevHostDirVolMount,
			KubeSANNodeLocalControllerSocketVolMount,
		}
		containers = append(containers, corev1.Container{
			Name:    "kubesan-csi-controller-plugin",
			Image:   constants.KubeSANImage,
			Command: []string{"./kubesan", "csi-controller-plugin"},
			SecurityContext: &corev1.SecurityContext{
				Privileged: ptr.To(true),
			},
			VolumeMounts: kubeSANControllerVolumeMounts,
			Env: []corev1.EnvVar{
				{
					Name:  "KUBESAN_IMAGE",
					Value: constants.KubeSANImage,
				},
				{
					Name: "NAMESPACE",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.namespace",
						},
					},
				},
				{
					Name: "NODE_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "spec.nodeName",
						},
					},
				},
			},
		})
	}

	annotations := map[string]string{
		constants.WorkloadPartitioningManagementAnnotation: constants.ManagementAnnotationVal,
	}
	labels := map[string]string{
		constants.AppKubernetesNameLabel:      constants.VGManagerLabelVal,
		constants.AppKubernetesManagedByLabel: constants.ManagedByLabelVal,
		constants.AppKubernetesPartOfLabel:    constants.PartOfLabelVal,
		constants.AppKubernetesComponentLabel: constants.VGManagerLabelVal,
	}
	ds := appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VGManagerUnit,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: annotations,
					Labels:      labels,
				},

				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: ptr.To(int64(30)),
					PriorityClassName:             constants.PriorityClassNameUserCritical,
					Volumes:                       volumes,
					Containers:                    containers,
					HostPID:                       true,
					Tolerations:                   tolerations,
					ServiceAccountName:            constants.VGManagerServiceAccount,
				},
			},
		},
	}

	// set nodeSelector
	setDaemonsetNodeSelector(nodeSelector, &ds)
	return ds
}

func GetAbsoluteKubeletPath(name string) string {
	if strings.HasSuffix(name, "/") {
		return name
	} else {
		return name + "/"
	}
}
