package controllers

import (
	"context"
	"fmt"
	"path/filepath"

	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	controllerName = "topolvm-controller"
)

type topolvmController struct{}

// topolvmController unit satisfies resourceManager interface
var _ resourceManager = topolvmController{}

func (c topolvmController) getName() string {
	return controllerName
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=create;update;delete;get;list;watch

func (c topolvmController) ensureCreated(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {

	// get the desired state of topolvm controller deployment
	desiredDeployment := getControllerDeployment(lvmCluster, r.Namespace, r.ImageName)
	existingDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desiredDeployment.Name,
			Namespace: desiredDeployment.Namespace,
		},
	}

	result, err := cutil.CreateOrUpdate(ctx, r.Client, existingDeployment, func() error {
		return c.setTopolvmControllerDesiredState(existingDeployment, desiredDeployment)
	})

	if err != nil {
		r.Log.Error(err, "csi controller reconcile failure", "name", desiredDeployment.Name)
	} else {
		r.Log.Info("csi controller", "operation", result, "name", desiredDeployment.Name)
	}

	return err
}

func (c topolvmController) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	existingDeployment := &appsv1.Deployment{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: TopolvmControllerDeploymentName, Namespace: r.Namespace}, existingDeployment)

	if err != nil {
		// already deleted in previous reconcile
		if errors.IsNotFound(err) {
			r.Log.Info("csi controller deleted", "TopolvmController", existingDeployment.Name)
			return nil
		}
		r.Log.Error(err, "failed to retrieve csi controller deployment", "TopolvmController", existingDeployment.Name)
		return err
	}

	// if not deleted, initiate deletion
	if existingDeployment.GetDeletionTimestamp().IsZero() {
		if err = r.Client.Delete(ctx, existingDeployment); err != nil {
			r.Log.Error(err, "failed to delete topolvm controller deployment", "TopolvmController", existingDeployment.Name)
			return err
		} else {
			r.Log.Info("initiated topolvm controller deployment deletion", "TopolvmController", existingDeployment.Name)
		}
	} else {
		// set deletion in-progress for next reconcile to confirm deletion
		return fmt.Errorf("topolvm controller deployment %s is already marked for deletion", existingDeployment.Name)
	}

	return err
}

func (c topolvmController) updateStatus(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	// TODO: Verify the status of controller plugin deployment and set the same on CR
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

	return nil
}

func getControllerDeployment(lvmCluster *lvmv1alpha1.LVMCluster, namespace string, initImage string) *appsv1.Deployment {
	// Topolvm CSI Controller Deployment
	var replicas int32 = 1
	volumes := []corev1.Volume{
		{Name: "socket-dir", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "certs", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	// TODO: Remove custom generation of TLS certs, current it's being used in topolvm controller manager
	initContainers := []corev1.Container{
		*getInitContainer(initImage),
	}

	// get all containers that are part of csi controller deployment
	containers := []corev1.Container{
		*getControllerContainer(),
		*getCsiProvisionerContainer(),
		*getCsiResizerContainer(),
		*getLivenessProbeContainer(),
	}

	labels := map[string]string{
		DefaultLabelKey: TopolvmControllerLabelVal,
	}

	controllerDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TopolvmControllerDeploymentName,
			Namespace: namespace,
			Labels:    labels,
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
	return controllerDeployment
}

func getInitContainer(initImage string) *corev1.Container {

	// generation of tls certs
	command := []string{
		"/usr/bin/bash",
		"-c",
		"openssl req -nodes -x509 -newkey rsa:4096 -subj '/DC=self_signed_certificate' -keyout /certs/tls.key -out /certs/tls.crt -days 3650",
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "certs", MountPath: "/certs"},
	}

	ssCertGenerator := &corev1.Container{
		Name:         "self-signed-cert-generator",
		Image:        initImage,
		Command:      command,
		VolumeMounts: volumeMounts,
	}

	return ssCertGenerator
}

func getControllerContainer() *corev1.Container {

	// topolvm controller plugin container
	command := []string{
		"/topolvm-controller",
		"--cert-dir=/certs",
	}

	resourceRequirements := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(TopolvmControllerCPULimit),
			corev1.ResourceMemory: resource.MustParse(TopolvmControllerMemLimit),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(TopolvmControllerCPURequest),
			corev1.ResourceMemory: resource.MustParse(TopolvmControllerMemRequest),
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "socket-dir", MountPath: filepath.Dir(DefaultCSISocket)},
		{Name: "certs", MountPath: "/certs"},
	}

	controller := &corev1.Container{
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
	return controller
}

func getCsiProvisionerContainer() *corev1.Container {

	// csi provisioner container
	args := []string{
		fmt.Sprintf("--csi-address=%s", DefaultCSISocket),
		"--enable-capacity",
		"--capacity-ownerref-level=2",
		"--capacity-poll-interval=30s",
		"--feature-gates=Topology=true",
	}

	resourceRequirements := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(TopolvmCsiProvisionerCPULimit),
			corev1.ResourceMemory: resource.MustParse(TopolvmCsiProvisionerMemLimit),
		},
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

	csiProvisioner := &corev1.Container{
		Name:         CsiProvisionerContainerName,
		Image:        CsiProvisionerImage,
		Args:         args,
		Resources:    resourceRequirements,
		VolumeMounts: volumeMounts,
		Env:          env,
	}
	return csiProvisioner
}

func getCsiResizerContainer() *corev1.Container {

	// csi resizer container
	args := []string{
		fmt.Sprintf("--csi-address=%s", DefaultCSISocket),
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "socket-dir", MountPath: filepath.Dir(DefaultCSISocket)},
	}

	csiResizer := &corev1.Container{
		Name:         CsiResizerContainerName,
		Image:        CsiResizerImage,
		Args:         args,
		VolumeMounts: volumeMounts,
	}
	return csiResizer
}

func getLivenessProbeContainer() *corev1.Container {

	// csi liveness probe container
	args := []string{
		fmt.Sprintf("--csi-address=%s", DefaultCSISocket),
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "socket-dir", MountPath: filepath.Dir(DefaultCSISocket)},
	}

	livenessProbe := &corev1.Container{
		Name:         CsiLivenessProbeContainerName,
		Image:        CsiLivenessProbeImage,
		Args:         args,
		VolumeMounts: volumeMounts,
	}
	return livenessProbe
}
