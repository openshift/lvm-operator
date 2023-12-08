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
	"context"
	"fmt"
	"path/filepath"

	v1 "github.com/openshift/api/config/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/internal/controllers/constants"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var volumeMountsWithCSISocket = []corev1.VolumeMount{
	{Name: "socket-dir", MountPath: filepath.Dir(constants.DefaultCSISocket)},
}

var controllerLabels = map[string]string{
	constants.AppKubernetesNameLabel:      constants.CsiDriverNameVal,
	constants.AppKubernetesManagedByLabel: constants.ManagedByLabelVal,
	constants.AppKubernetesPartOfLabel:    constants.PartOfLabelVal,
	constants.AppKubernetesComponentLabel: constants.TopolvmControllerLabelVal,
}

func TopoLVMController() Manager {
	return topolvmController{}
}

type topolvmController struct{}

// topolvmController unit satisfies resourceManager interface
var _ Manager = topolvmController{}

func (c topolvmController) GetName() string {
	return constants.TopolvmControllerDeploymentName
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=create;update;delete;get;list;watch

func (c topolvmController) EnsureCreated(r Reconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", c.GetName())

	// get the desired state of topolvm controller deployment
	desiredDeployment := getControllerDeployment(
		r.GetNamespace(),
		r.SnapshotsEnabled(),
		r.GetTopoLVMLeaderElectionPassthrough(),
		r.GetLogPassthroughOptions().TopoLVMController.AsArgs(),
		r.GetLogPassthroughOptions().CSISideCar.AsArgs(),
	)
	existingDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desiredDeployment.Name,
			Namespace: desiredDeployment.Namespace,
		},
	}

	result, err := cutil.CreateOrUpdate(ctx, r, existingDeployment, func() error {
		if err := cutil.SetControllerReference(lvmCluster, existingDeployment, r.Scheme()); err != nil {
			return fmt.Errorf("failed to set controller reference for csi controller: %w", err)
		}
		// at creation, deep copy desired deployment into existing
		if existingDeployment.CreationTimestamp.IsZero() {
			desiredDeployment.DeepCopyInto(existingDeployment)
			return nil
		}

		// for update, topolvm controller is interested in only updating container images
		// labels, volumes, service account etc can remain unchanged
		existingDeployment.Spec.Template.Spec.Containers = desiredDeployment.Spec.Template.Spec.Containers

		existingDeployment.Spec.Template.Spec.PriorityClassName = desiredDeployment.Spec.Template.Spec.PriorityClassName

		initMapIfNil(&existingDeployment.ObjectMeta.Annotations)
		for key, value := range desiredDeployment.Annotations {
			existingDeployment.ObjectMeta.Annotations[key] = value
		}

		initMapIfNil(&existingDeployment.Spec.Template.Annotations)
		for key, value := range desiredDeployment.Spec.Template.Annotations {
			existingDeployment.Spec.Template.Annotations[key] = value
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("could not create/update csi controller: %w", err)
	}
	logger.Info("Deployment applied to cluster", "operation", result, "name", desiredDeployment.Name)

	if err := verifyDeploymentReadiness(existingDeployment); err != nil {
		return fmt.Errorf("csi controller is not ready: %w", err)
	}
	logger.Info("Deployment is ready", "name", desiredDeployment.Name)

	return nil
}

// ensureDeleted is a noop. Deletion will be handled by ownerref
func (c topolvmController) EnsureDeleted(_ Reconciler, _ context.Context, _ *lvmv1alpha1.LVMCluster) error {
	return nil
}

func getControllerDeployment(namespace string, enableSnapshots bool, topoLVMLeaderElectionPassthrough v1.LeaderElection, args []string, csiArgs []string) *appsv1.Deployment {
	// Topolvm CSI Controller Deployment
	var replicas int32 = 1
	volumes := []corev1.Volume{
		{Name: "socket-dir", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	// get all containers that are part of csi controller deployment
	containers := []corev1.Container{
		controllerContainer(topoLVMLeaderElectionPassthrough, args),
		csiProvisionerContainer(topoLVMLeaderElectionPassthrough, csiArgs),
		csiResizerContainer(topoLVMLeaderElectionPassthrough, csiArgs),
		livenessProbeContainer(),
	}

	if enableSnapshots {
		containers = append(containers, csiSnapshotterContainer(topoLVMLeaderElectionPassthrough, csiArgs))
	}

	annotations := map[string]string{
		constants.WorkloadPartitioningManagementAnnotation: constants.ManagementAnnotationVal,
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.TopolvmControllerDeploymentName,
			Namespace: namespace,
			Labels:    controllerLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: controllerLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        constants.TopolvmControllerDeploymentName,
					Namespace:   namespace,
					Annotations: annotations,
					Labels:      controllerLabels,
				},
				Spec: corev1.PodSpec{
					Containers:         containers,
					ServiceAccountName: constants.TopolvmControllerServiceAccount,
					PriorityClassName:  constants.PriorityClassNameClusterCritical,
					Volumes:            volumes,
				},
			},
		},
	}
}

func controllerContainer(topoLVMLeaderElectionPassthrough v1.LeaderElection, args []string) corev1.Container {

	// topolvm controller plugin container
	command := []string{
		"/topolvm-controller",
		"--enable-webhooks=false",
		fmt.Sprintf("--leader-election-namespace=%s", topoLVMLeaderElectionPassthrough.Namespace),
		fmt.Sprintf("--leader-election-lease-duration=%s", topoLVMLeaderElectionPassthrough.LeaseDuration.Duration),
		fmt.Sprintf("--leader-election-renew-deadline=%s", topoLVMLeaderElectionPassthrough.RenewDeadline.Duration),
		fmt.Sprintf("--leader-election-retry-period=%s", topoLVMLeaderElectionPassthrough.RetryPeriod.Duration),
	}

	command = append(command, args...)

	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(constants.TopolvmControllerCPURequest),
			corev1.ResourceMemory: resource.MustParse(constants.TopolvmControllerMemRequest),
		},
	}

	return corev1.Container{
		Name:    constants.TopolvmControllerDeploymentName,
		Image:   TopolvmCsiImage,
		Command: command,
		Ports: []corev1.ContainerPort{
			{
				Name:          constants.TopolvmControllerContainerHealthzName,
				ContainerPort: constants.TopolvmControllerContainerLivenessPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromString(constants.TopolvmControllerContainerHealthzName),
				},
			},
			FailureThreshold:    3,
			InitialDelaySeconds: 10,
			TimeoutSeconds:      3,
			PeriodSeconds:       60,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/metrics",
					Port: intstr.IntOrString{
						IntVal: constants.TopolvmControllerContainerReadinessPort,
					},
					Scheme: corev1.URISchemeHTTP,
				},
			},
		},
		Resources:    resourceRequirements,
		VolumeMounts: volumeMountsWithCSISocket,
	}
}

func csiProvisionerContainer(topoLVMLeaderElectionPassthrough v1.LeaderElection, additionalArgs []string) corev1.Container {

	// csi provisioner container
	args := []string{
		fmt.Sprintf("--csi-address=%s", constants.DefaultCSISocket),
		"--enable-capacity",
		"--capacity-ownerref-level=2",
		"--capacity-poll-interval=30s",
		"--feature-gates=Topology=true",
		fmt.Sprintf("--leader-election-namespace=%s", topoLVMLeaderElectionPassthrough.Namespace),
		fmt.Sprintf("--leader-election-lease-duration=%s", topoLVMLeaderElectionPassthrough.LeaseDuration.Duration),
		fmt.Sprintf("--leader-election-renew-deadline=%s", topoLVMLeaderElectionPassthrough.RenewDeadline.Duration),
		fmt.Sprintf("--leader-election-retry-period=%s", topoLVMLeaderElectionPassthrough.RetryPeriod.Duration),
	}

	args = append(args, additionalArgs...)

	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(constants.TopolvmCsiProvisionerCPURequest),
			corev1.ResourceMemory: resource.MustParse(constants.TopolvmCsiProvisionerMemRequest),
		},
	}

	return corev1.Container{
		Name:         constants.CsiProvisionerContainerName,
		Image:        CsiProvisionerImage,
		Args:         args,
		Resources:    resourceRequirements,
		VolumeMounts: volumeMountsWithCSISocket,
		// CSI Provisioner requires below environment values to make use of CSIStorageCapacity
		Env: defaultEnvVars,
	}
}

func csiResizerContainer(topoLVMLeaderElectionPassthrough v1.LeaderElection, additionalArgs []string) corev1.Container {

	// csi resizer container
	args := []string{
		fmt.Sprintf("--csi-address=%s", constants.DefaultCSISocket),
		fmt.Sprintf("--leader-election-namespace=%s", topoLVMLeaderElectionPassthrough.Namespace),
		fmt.Sprintf("--leader-election-lease-duration=%s", topoLVMLeaderElectionPassthrough.LeaseDuration.Duration),
		fmt.Sprintf("--leader-election-renew-deadline=%s", topoLVMLeaderElectionPassthrough.RenewDeadline.Duration),
		fmt.Sprintf("--leader-election-retry-period=%s", topoLVMLeaderElectionPassthrough.RetryPeriod.Duration),
	}

	args = append(args, additionalArgs...)

	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(constants.TopolvmCsiResizerCPURequest),
			corev1.ResourceMemory: resource.MustParse(constants.TopolvmCsiResizerMemRequest),
		},
	}

	return corev1.Container{
		Name:         constants.CsiResizerContainerName,
		Image:        CsiResizerImage,
		Args:         args,
		Resources:    resourceRequirements,
		VolumeMounts: volumeMountsWithCSISocket,
	}
}

func csiSnapshotterContainer(topoLVMLeaderElectionPassthrough v1.LeaderElection, additionalArgs []string) corev1.Container {

	args := []string{
		fmt.Sprintf("--csi-address=%s", constants.DefaultCSISocket),
		fmt.Sprintf("--leader-election-namespace=%s", topoLVMLeaderElectionPassthrough.Namespace),
		fmt.Sprintf("--leader-election-lease-duration=%s", topoLVMLeaderElectionPassthrough.LeaseDuration.Duration),
		fmt.Sprintf("--leader-election-renew-deadline=%s", topoLVMLeaderElectionPassthrough.RenewDeadline.Duration),
		fmt.Sprintf("--leader-election-retry-period=%s", topoLVMLeaderElectionPassthrough.RetryPeriod.Duration),
	}

	args = append(args, additionalArgs...)

	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(constants.TopolvmCsiSnapshotterCPURequest),
			corev1.ResourceMemory: resource.MustParse(constants.TopolvmCsiSnapshotterMemRequest),
		},
	}

	return corev1.Container{
		Name:         constants.CsiSnapshotterContainerName,
		Image:        CsiSnapshotterImage,
		Args:         args,
		VolumeMounts: volumeMountsWithCSISocket,
		Resources:    resourceRequirements,
	}
}

func livenessProbeContainer() corev1.Container {
	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(constants.LivenessProbeCPURequest),
			corev1.ResourceMemory: resource.MustParse(constants.LivenessProbeMemRequest),
		},
	}

	// csi liveness probe container
	args := []string{
		fmt.Sprintf("--csi-address=%s", constants.DefaultCSISocket),
	}

	return corev1.Container{
		Name:         constants.CsiLivenessProbeContainerName,
		Image:        CsiLivenessProbeImage,
		Args:         args,
		VolumeMounts: volumeMountsWithCSISocket,
		Resources:    resourceRequirements,
	}
}
