package controllers

import (
	"context"
	"fmt"

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

type controllerPlugin struct{}

// controllerPlugin unit satisfies resourceManager interface
var _ resourceManager = controllerPlugin{}

func (c controllerPlugin) getName() string {
	return controllerName
}

func (c controllerPlugin) ensureCreated(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {

	// get the desired state of topolvm controller deployment
	controllerDeployment := getControllerDeployment(lvmCluster)
	result, err := cutil.CreateOrUpdate(ctx, r.Client, controllerDeployment, func() error {
		// make sure LVMCluster CR garbage collects controller deployments and also block owner removal
		return cutil.SetOwnerReference(lvmCluster, controllerDeployment, r.Scheme)
	})

	// log the result based on current reconcile
	switch result {
	case cutil.OperationResultCreated:
		r.Log.Info("csi controller", "operation", result, "name", controllerDeployment.Name)
	case cutil.OperationResultUpdated:
		r.Log.Info("csi controller", "operation", result, "name", controllerDeployment.Name)
	case cutil.OperationResultNone:
		r.Log.Info("csi controller", "operation", result, "name", controllerDeployment.Name)
	default:
		r.Log.Error(err, "csi controller reconcile failure", "name", controllerDeployment.Name)
		return err
	}

	return nil
}

func (c controllerPlugin) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	controllerDeployment := &appsv1.Deployment{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: TopolvmControllerDeploymentName, Namespace: lvmCluster.Namespace}, controllerDeployment)

	if err != nil {
		// already deleted in previous reconcile
		if errors.IsNotFound(err) {
			r.Log.Info("csi controller deleted", "TopolvmController", controllerDeployment.Name)
			return nil
		}
		r.Log.Error(err, "unable to retrieve csi controller deployment", "TopolvmController", controllerDeployment.Name)
		return err
	}

	// if not deleted, initiate deletion
	if controllerDeployment.GetDeletionTimestamp().IsZero() {
		if err = r.Client.Delete(ctx, controllerDeployment); err != nil {
			r.Log.Error(err, "unable to delete topolvm controller deployment", "TopolvmController", controllerDeployment.Name)
			return err
		}
	} else {
		// set deletion in-progress for next reconcile to confirm deletion
		return fmt.Errorf("topolvm controller deployment %s is being uninstalled", controllerDeployment.Name)
	}

	return nil
}

func (c controllerPlugin) updateStatus(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	// TODO: Verify the status of controller plugin deployment and set the same on CR
	return nil
}

func getControllerDeployment(lvmCluster *lvmv1alpha1.LVMCluster) *appsv1.Deployment {

	// Topolvm CSI Controller Deployment
	var replicas int32 = 1
	volumes := []corev1.Volume{
		{Name: "socket-dir", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "certs", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	// TODO: Remove custom generation of TLS certs, find where it's being used in the first place in Topolvm Code
	iContainers := []corev1.Container{*getInitContainer()}

	// getall containers that are part of csi controller deployment
	containers := []corev1.Container{*getControllerContainer(), *getCsiProvisionerContainer(), *getCsiResizerContainer(), *getLivenessProbeContainer()}
	controllerDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TopolvmControllerDeploymentName,
			Namespace: lvmCluster.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					AppAttr: TopolvmControllerDeploymentName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      TopolvmControllerDeploymentName,
					Namespace: lvmCluster.Namespace,
					Labels: map[string]string{
						AppAttr: TopolvmControllerDeploymentName,
					},
				},
				Spec: corev1.PodSpec{
					InitContainers:     iContainers,
					Containers:         containers,
					ServiceAccountName: TopolvmControllerServiceAccount,
					Volumes:            volumes,
				},
			},
		},
	}
	return controllerDeployment
}

func getInitContainer() *corev1.Container {

	// generation of tls certs
	command := []string{
		"sh",
		"-c",
		"openssl req -nodes -x509 -newkey rsa:4096 -subj '/DC=self_signed_certificate' -keyout /certs/tls.key -out /certs/tls.crt -days 365",
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "certs", MountPath: "/certs"},
	}

	ssCertGenerator := &corev1.Container{
		Name:         "self-signed-cert-generator",
		Image:        "alpine/openssl",
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
		{Name: "socket-dir", MountPath: "/run/topolvm"},
		{Name: "certs", MountPath: "/certs"},
	}

	controller := &corev1.Container{
		Name:    TopolvmControllerContainerName,
		Image:   TopolvmCsiImage,
		Command: command,
		Ports:   []corev1.ContainerPort{{Name: TopolvmControllerContainerHealthzName, ContainerPort: TopolvmControllerContainerLivenessPort, Protocol: corev1.ProtocolTCP}},
		LivenessProbe: &corev1.Probe{Handler: corev1.Handler{HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromString(TopolvmControllerContainerHealthzName)}},
			FailureThreshold: 3, InitialDelaySeconds: 10, TimeoutSeconds: 3, PeriodSeconds: 60},
		ReadinessProbe: &corev1.Probe{Handler: corev1.Handler{HTTPGet: &corev1.HTTPGetAction{Path: "/metrics", Port: intstr.IntOrString{IntVal: TopolvmControllerContainerReadinessPort}, Scheme: corev1.URISchemeHTTP}}},
		Resources:      resourceRequirements,
		VolumeMounts:   volumeMounts,
	}
	return controller
}
func getCsiProvisionerContainer() *corev1.Container {

	// csi provisioner container
	command := []string{"/csi-provisioner",
		"--csi-address=/run/topolvm/csi-topolvm.sock",
		"--enable-capacity",
		"--capacity-ownerref-level=2",
		"--capacity-poll-interval=30s",
		"--feature-gates=Topology=true",
	}

	resourceRequirements := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(TopolvmControllerCsiProvisionCPULimit),
			corev1.ResourceMemory: resource.MustParse(TopolvmControllerCsiProvisionMemLimit),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(TopolvmControllerCsiProvisionCPURequest),
			corev1.ResourceMemory: resource.MustParse(TopolvmControllerCsiProvisionMemRequest),
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "socket-dir", MountPath: "/run/topolvm"},
	}

	csiProvisioner := &corev1.Container{
		Name:         TopolvmCsiProvisionerContainerName,
		Image:        CsiProvisionerImage,
		Command:      command,
		Resources:    resourceRequirements,
		VolumeMounts: volumeMounts,
	}
	return csiProvisioner
}

func getCsiResizerContainer() *corev1.Container {

	// csi resizer container
	command := []string{
		"/csi-resizer",
		"--csi-address=/run/topolvm/csi-topolvm.sock",
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "socket-dir", MountPath: "/run/topolvm"},
	}

	csiResizer := &corev1.Container{
		Name:         TopolvmCsiResizerContainerName,
		Image:        CsiResizerImage,
		Command:      command,
		VolumeMounts: volumeMounts,
	}
	return csiResizer
}

func getLivenessProbeContainer() *corev1.Container {

	// csi liveness probe container
	command := []string{
		"/livenessprobe",
		"--csi-address=/run/topolvm/csi-topolvm.sock",
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "socket-dir", MountPath: "/run/topolvm"},
	}

	livenessProbe := &corev1.Container{
		Name:         TopolvmCsiLivenessProbeContainerName,
		Image:        CsiLivenessProbeImage,
		Command:      command,
		VolumeMounts: volumeMounts,
	}
	return livenessProbe
}
