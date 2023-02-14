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

import (
	"os"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	hostContainerPropagation  = corev1.MountPropagationHostToContainer
	directoryHostPath         = corev1.HostPathDirectory
	HostPathDirectoryOrCreate = corev1.HostPathDirectoryOrCreate

	LVMdVolName   = "lvmd-conf"
	UdevVolName   = "run-udev"
	DevDirVolName = "device-dir"
	SysVolName    = "sys"

	LVMdDir    = "/etc/topolvm"
	devDirPath = "/dev"
	udevPath   = "/run/udev"
	sysPath    = "/sys"

	// LVMDConfVol is the corev1.Volume definition for the directory on host ("/etc/topolvm") for storing
	// the lvmd.conf file
	LVMDConfVol = corev1.Volume{
		Name: LVMdVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: LVMdDir,
				Type: &HostPathDirectoryOrCreate,
			},
		},
	}

	// LVMDConfVolMount is the corresponding mount for LVMDConfVol
	LVMDConfVolMount = corev1.VolumeMount{
		Name:             LVMdVolName,
		MountPath:        LVMdDir,
		MountPropagation: &hostContainerPropagation,
	}

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

	// DevMount is the corresponding mount for DevHostDirVol
	DevHostDirVolMount = corev1.VolumeMount{
		Name:             DevDirVolName,
		MountPath:        devDirPath,
		MountPropagation: &hostContainerPropagation,
	}

	// UDevHostDirVol is the corev1.Volume definition for the
	// "/run/udev" host bind-mount. This helps lsblk give more accurate output.
	// UDevMount is the corresponding mount
	UDevHostDirVol = corev1.Volume{
		Name: UdevVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: udevPath},
		},
	}

	// UDevMount is the corresponding mount for UDevHostDirVol
	UDevHostDirVolMount = corev1.VolumeMount{
		Name:             UdevVolName,
		MountPath:        udevPath,
		MountPropagation: &hostContainerPropagation,
	}

	// SysHostDirVol is the corev1.Volume definition for the
	// "/sys" host bind-mount. This helps discover information about blockd devices
	SysHostDirVol = corev1.Volume{
		Name: SysVolName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: sysPath},
		},
	}

	// SysMount is the corresponding mount for SysHostDirVol
	SysHostDirVolMount = corev1.VolumeMount{
		Name:             SysVolName,
		MountPath:        sysPath,
		MountPropagation: &hostContainerPropagation,
	}
)

// newVGManagerDaemonset returns the desired vgmanager daemonset for a given LVMCluster
func newVGManagerDaemonset(lvmCluster *lvmv1alpha1.LVMCluster, namespace string, vgImage string) appsv1.DaemonSet {
	// aggregate nodeSelector and tolerations from all deviceClasses
	nodeSelector, tolerations := extractNodeSelectorAndTolerations(lvmCluster)
	volumes := []corev1.Volume{LVMDConfVol, DevHostDirVol, UDevHostDirVol, SysHostDirVol}
	volumeMounts := []corev1.VolumeMount{LVMDConfVolMount, DevHostDirVolMount, UDevHostDirVolMount, SysHostDirVolMount}
	privileged := true
	var zero int64 = 0

	// try to get vgmanager image from env and on absence get from running pod
	// TODO: investigate why storing this env in a variable is failing tests
	image := os.Getenv("VGMANAGER_IMAGE")
	if image == "" {
		image = vgImage
	}

	command := []string{
		"/vgmanager",
	}

	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(VgManagerCPURequest),
			corev1.ResourceMemory: resource.MustParse(VgManagerMemRequest),
		},
	}
	containers := []corev1.Container{
		{
			Name:    VGManagerUnit,
			Image:   image,
			Command: command,
			SecurityContext: &corev1.SecurityContext{
				Privileged: &privileged,
				RunAsUser:  &zero,
			},
			VolumeMounts: volumeMounts,
			Resources:    resourceRequirements,
			Env: []corev1.EnvVar{
				{
					Name: "NODE_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "spec.nodeName",
						},
					},
				},
				{
					Name: "POD_NAMESPACE",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.namespace",
						},
					},
				},
				{
					Name: "POD_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
					},
				},
			},
		},
	}
	annotations := map[string]string{
		workloadPartitioningManagementAnnotation: managementAnnotationVal,
	}
	labels := map[string]string{
		AppKubernetesNameLabel:      VGManagerLabelVal,
		AppKubernetesManagedByLabel: ManagedByLabelVal,
		AppKubernetesPartOfLabel:    PartOfLabelVal,
		AppKubernetesComponentLabel: VGManagerLabelVal,
	}
	ds := appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        VGManagerUnit,
			Namespace:   namespace,
			Annotations: annotations,
			Labels:      labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},

				Spec: corev1.PodSpec{
					Volumes:    volumes,
					Containers: containers,
					// to read /proc/1/mountinfo
					HostPID:            true,
					Tolerations:        tolerations,
					ServiceAccountName: VGManagerServiceAccount,
				},
			},
		},
	}

	// set nodeSelector
	setDaemonsetNodeSelector(nodeSelector, &ds)
	return ds
}
