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

package controllers

import (
	"context"
	"fmt"
	"path/filepath"

	v1 "github.com/openshift/api/config/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serror "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	controllerName = "topolvm-controller"
)

type topolvmController struct {
	topoLVMLeaderElectionPassthrough v1.LeaderElection
}

// topolvmController unit satisfies resourceManager interface
var _ resourceManager = topolvmController{}

func (c topolvmController) getName() string {
	return controllerName
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=create;update;delete;get;list;watch

func (c topolvmController) ensureCreated(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {

	// get the desired state of topolvm controller deployment
	desiredDeployment := getControllerDeployment(lvmCluster, r.Namespace, r.ImageName, c.topoLVMLeaderElectionPassthrough)
	existingDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desiredDeployment.Name,
			Namespace: desiredDeployment.Namespace,
		},
	}

	err := cutil.SetControllerReference(lvmCluster, existingDeployment, r.Scheme)
	if err != nil {
		r.Log.Error(err, "failed to set controller reference to topolvm controller deployment with name", existingDeployment.Name)
		return err
	}

	result, err := cutil.CreateOrUpdate(ctx, r.Client, existingDeployment, func() error {
		return c.setTopolvmControllerDesiredState(existingDeployment, desiredDeployment)
	})

	if err != nil {
		r.Log.Error(err, "csi controller reconcile failure", "name", desiredDeployment.Name)
		return err
	}

	if err := verifyDeploymentReadiness(existingDeployment); err != nil {
		r.Log.Error(err, "csi controller is not considered ready", "deployment", existingDeployment.Name)
		return err
	}

	r.Log.Info("csi controller", "operation", result, "name", desiredDeployment.Name)
	return nil
}

func (c topolvmController) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	existingDeployment := &appsv1.Deployment{}

	err := r.Client.Get(ctx, types.NamespacedName{Name: TopolvmControllerDeploymentName, Namespace: r.Namespace}, existingDeployment)
	if err != nil {
		// already deleted in previous reconcile
		if k8serror.IsNotFound(err) {
			r.Log.Info("csi controller deleted", "TopolvmController", existingDeployment.Name)
			return nil
		}
		r.Log.Error(err, "failed to retrieve csi controller deployment", "TopolvmController", existingDeployment.Name)
		return err
	}

	// if not deleted, initiate deletion
	if !existingDeployment.GetDeletionTimestamp().IsZero() {
		// set deletion in-progress for next reconcile to confirm deletion
		return errors.Errorf("topolvm controller deployment %s is already marked for deletion", existingDeployment.Name)
	}

	err = r.Client.Delete(ctx, existingDeployment)
	if err != nil {
		r.Log.Error(err, "failed to delete topolvm controller deployment", "TopolvmController", existingDeployment.Name)
		return err
	}

	r.Log.Info("initiated topolvm controller deployment deletion", "TopolvmController", existingDeployment.Name)
	return nil
}

func (c topolvmController) setTopolvmControllerDesiredState(existing, desired *appsv1.Deployment) error {

	// at creation, deep copy desired deployment into existing
	if existing.CreationTimestamp.IsZero() {
		desired.DeepCopyInto(existing)
		return nil
	}

	// for update, topolvm controller is interested in only updating container images
	// labels, volumes, service account etc can remain unchanged
	existing.Spec.Template.Spec.Containers = desired.Spec.Template.Spec.Containers
	existing.Spec.Template.Spec.InitContainers = desired.Spec.Template.Spec.InitContainers

	return nil
}

func getControllerDeployment(lvmCluster *lvmv1alpha1.LVMCluster, namespace string, initImage string, topoLVMLeaderElectionPassthrough v1.LeaderElection) *appsv1.Deployment {
	// Topolvm CSI Controller Deployment
	var replicas int32 = 1
	volumes := []corev1.Volume{
		{Name: "socket-dir", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "certs", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	initContainers := []corev1.Container{
		initContainer(initImage),
	}

	// get all containers that are part of csi controller deployment
	containers := []corev1.Container{
		controllerContainer(topoLVMLeaderElectionPassthrough),
		csiProvisionerContainer(topoLVMLeaderElectionPassthrough),
		csiResizerContainer(topoLVMLeaderElectionPassthrough),
		livenessProbeContainer(),
		csiSnapshotterContainer(topoLVMLeaderElectionPassthrough),
	}

	annotations := map[string]string{
		workloadPartitioningManagementAnnotation: managementAnnotationVal,
	}

	labels := map[string]string{
		AppKubernetesNameLabel:      CsiDriverNameVal,
		AppKubernetesManagedByLabel: ManagedByLabelVal,
		AppKubernetesPartOfLabel:    PartOfLabelVal,
		AppKubernetesComponentLabel: TopolvmControllerLabelVal,
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        TopolvmControllerDeploymentName,
			Namespace:   namespace,
			Annotations: annotations,
			Labels:      labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      TopolvmControllerDeploymentName,
					Namespace: namespace,
					Labels:    labels,
				},
				Spec: corev1.PodSpec{
					InitContainers:     initContainers,
					Containers:         containers,
					ServiceAccountName: TopolvmControllerServiceAccount,
					Volumes:            volumes,
				},
			},
		},
	}
}

func initContainer(initImage string) corev1.Container {

	// generation of tls certs
	command := []string{
		"/usr/bin/bash",
		"-c",
		"openssl req -nodes -x509 -newkey rsa:4096 -subj '/DC=self_signed_certificate' -keyout /certs/tls.key -out /certs/tls.crt -days 3650",
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "certs", MountPath: "/certs"},
	}

	return corev1.Container{
		Name:         "self-signed-cert-generator",
		Image:        initImage,
		Command:      command,
		VolumeMounts: volumeMounts,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(CertGeneratorCPURequest),
				corev1.ResourceMemory: resource.MustParse(CertGeneratorMemRequest),
			},
		},
	}
}

func controllerContainer(topoLVMLeaderElectionPassthrough v1.LeaderElection) corev1.Container {

	// topolvm controller plugin container
	command := []string{
		"/topolvm-controller",
		"--cert-dir=/certs",
		fmt.Sprintf("--leader-election-namespace=%s", topoLVMLeaderElectionPassthrough.Namespace),
		fmt.Sprintf("--leader-election-lease-duration=%s", topoLVMLeaderElectionPassthrough.LeaseDuration.Duration),
		fmt.Sprintf("--leader-election-renew-deadline=%s", topoLVMLeaderElectionPassthrough.RenewDeadline.Duration),
		fmt.Sprintf("--leader-election-retry-period=%s", topoLVMLeaderElectionPassthrough.RetryPeriod.Duration),
	}

	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(TopolvmControllerCPURequest),
			corev1.ResourceMemory: resource.MustParse(TopolvmControllerMemRequest),
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "socket-dir", MountPath: filepath.Dir(DefaultCSISocket)},
		{Name: "certs", MountPath: "/certs"},
	}

	return corev1.Container{
		Name:    TopolvmControllerContainerName,
		Image:   TopolvmCsiImage,
		Command: command,
		Ports: []corev1.ContainerPort{
			{
				Name:          TopolvmControllerContainerHealthzName,
				ContainerPort: TopolvmControllerContainerLivenessPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromString(TopolvmControllerContainerHealthzName),
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
						IntVal: TopolvmControllerContainerReadinessPort,
					},
					Scheme: corev1.URISchemeHTTP,
				},
			},
		},
		Resources:    resourceRequirements,
		VolumeMounts: volumeMounts,
	}
}

func csiProvisionerContainer(topoLVMLeaderElectionPassthrough v1.LeaderElection) corev1.Container {

	// csi provisioner container
	args := []string{
		fmt.Sprintf("--csi-address=%s", DefaultCSISocket),
		"--enable-capacity",
		"--capacity-ownerref-level=2",
		"--capacity-poll-interval=30s",
		"--feature-gates=Topology=true",
		fmt.Sprintf("--leader-election-namespace=%s", topoLVMLeaderElectionPassthrough.Namespace),
		fmt.Sprintf("--leader-election-lease-duration=%s", topoLVMLeaderElectionPassthrough.LeaseDuration.Duration),
		fmt.Sprintf("--leader-election-renew-deadline=%s", topoLVMLeaderElectionPassthrough.RenewDeadline.Duration),
		fmt.Sprintf("--leader-election-retry-period=%s", topoLVMLeaderElectionPassthrough.RetryPeriod.Duration),
	}

	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(TopolvmCsiProvisionerCPURequest),
			corev1.ResourceMemory: resource.MustParse(TopolvmCsiProvisionerMemRequest),
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "socket-dir", MountPath: filepath.Dir(DefaultCSISocket)},
	}

	env := []corev1.EnvVar{
		{
			Name: PodNameEnv,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: NameSpaceEnv,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	}

	return corev1.Container{
		Name:         CsiProvisionerContainerName,
		Image:        CsiProvisionerImage,
		Args:         args,
		Resources:    resourceRequirements,
		VolumeMounts: volumeMounts,
		Env:          env,
	}
}

func csiResizerContainer(topoLVMLeaderElectionPassthrough v1.LeaderElection) corev1.Container {

	// csi resizer container
	args := []string{
		fmt.Sprintf("--csi-address=%s", DefaultCSISocket),
		fmt.Sprintf("--leader-election-namespace=%s", topoLVMLeaderElectionPassthrough.Namespace),
		fmt.Sprintf("--leader-election-lease-duration=%s", topoLVMLeaderElectionPassthrough.LeaseDuration.Duration),
		fmt.Sprintf("--leader-election-renew-deadline=%s", topoLVMLeaderElectionPassthrough.RenewDeadline.Duration),
		fmt.Sprintf("--leader-election-retry-period=%s", topoLVMLeaderElectionPassthrough.RetryPeriod.Duration),
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "socket-dir", MountPath: filepath.Dir(DefaultCSISocket)},
	}

	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(TopolvmCsiResizerCPURequest),
			corev1.ResourceMemory: resource.MustParse(TopolvmCsiResizerMemRequest),
		},
	}

	return corev1.Container{
		Name:         CsiResizerContainerName,
		Image:        CsiResizerImage,
		Args:         args,
		Resources:    resourceRequirements,
		VolumeMounts: volumeMounts,
	}
}

func csiSnapshotterContainer(topoLVMLeaderElectionPassthrough v1.LeaderElection) corev1.Container {

	args := []string{
		fmt.Sprintf("--csi-address=%s", DefaultCSISocket),
		fmt.Sprintf("--leader-election-namespace=%s", topoLVMLeaderElectionPassthrough.Namespace),
		fmt.Sprintf("--leader-election-lease-duration=%s", topoLVMLeaderElectionPassthrough.LeaseDuration.Duration),
		fmt.Sprintf("--leader-election-renew-deadline=%s", topoLVMLeaderElectionPassthrough.RenewDeadline.Duration),
		fmt.Sprintf("--leader-election-retry-period=%s", topoLVMLeaderElectionPassthrough.RetryPeriod.Duration),
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "socket-dir", MountPath: filepath.Dir(DefaultCSISocket)},
	}

	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(TopolvmCsiSnapshotterCPURequest),
			corev1.ResourceMemory: resource.MustParse(TopolvmCsiSnapshotterMemRequest),
		},
	}

	return corev1.Container{
		Name:         CsiSnapshotterContainerName,
		Image:        CsiSnapshotterImage,
		Args:         args,
		VolumeMounts: volumeMounts,
		Resources:    resourceRequirements,
	}
}

func livenessProbeContainer() corev1.Container {
	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(LivenessProbeCPURequest),
			corev1.ResourceMemory: resource.MustParse(LivenessProbeMemRequest),
		},
	}

	// csi liveness probe container
	args := []string{
		fmt.Sprintf("--csi-address=%s", DefaultCSISocket),
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "socket-dir", MountPath: filepath.Dir(DefaultCSISocket)},
	}

	return corev1.Container{
		Name:         CsiLivenessProbeContainerName,
		Image:        CsiLivenessProbeImage,
		Args:         args,
		VolumeMounts: volumeMounts,
		Resources:    resourceRequirements,
	}
}
