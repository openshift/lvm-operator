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
	"strings"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/internal/controllers/constants"
	"github.com/openshift/lvm-operator/internal/controllers/lvmcluster/selector"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvmd"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	topolvmNodeName          = "topolvm-node"
	topolvmMetricsSecretName = "topolvm-metrics-cert"
)

func TopoLVMNode() Manager {
	return topolvmNode{}
}

type topolvmNode struct{}

func (n topolvmNode) GetName() string {
	return topolvmNodeName
}

//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=create;update;delete;get;list;watch

func (n topolvmNode) EnsureCreated(r Reconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("topolvmNode", n.GetName())

	// get desired daemonSet spec
	dsTemplate := getNodeDaemonSet(lvmCluster,
		r.GetNamespace(),
		r.GetLogPassthroughOptions().TopoLVMNode.AsArgs(),
		r.GetLogPassthroughOptions().CSISideCar.AsArgs(),
	)

	// create desired daemonSet or update mutable fields on existing one
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dsTemplate.Name,
			Namespace: dsTemplate.Namespace,
		},
	}

	logger.Info("running CreateOrUpdate")
	result, err := cutil.CreateOrUpdate(ctx, r, ds, func() error {
		if err := cutil.SetControllerReference(lvmCluster, ds, r.Scheme()); err != nil {
			return fmt.Errorf("failed to set controller reference to topolvm node daemonset: %w", err)
		}
		// at creation, deep copy the whole daemonSet
		if ds.CreationTimestamp.IsZero() {
			dsTemplate.DeepCopyInto(ds)
			return nil
		}
		// if update, update only mutable fields
		// For topolvm Node, we have containers, node selector and toleration terms

		// containers
		ds.Spec.Template.Spec.Containers = dsTemplate.Spec.Template.Spec.Containers

		// volumes
		ds.Spec.Template.Spec.Volumes = dsTemplate.Spec.Template.Spec.Volumes

		// tolerations
		ds.Spec.Template.Spec.Tolerations = dsTemplate.Spec.Template.Spec.Tolerations

		ds.Spec.Template.Spec.PriorityClassName = dsTemplate.Spec.Template.Spec.PriorityClassName

		// nodeSelector if non-nil
		if dsTemplate.Spec.Template.Spec.Affinity != nil {
			setDaemonsetNodeSelector(dsTemplate.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution, ds)
		}

		initMapIfNil(&ds.ObjectMeta.Annotations)
		for key, value := range dsTemplate.Annotations {
			ds.ObjectMeta.Annotations[key] = value
		}

		initMapIfNil(&ds.Spec.Template.Annotations)
		for key, value := range dsTemplate.Spec.Template.Annotations {
			ds.Spec.Template.Annotations[key] = value
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("%s failed to reconcile: %w", n.GetName(), err)
	}

	logger.Info("DaemonSet applied to cluster", "operation", result, "name", ds.Name)

	if err := verifyDaemonSetReadiness(ds); err != nil {
		return fmt.Errorf("DaemonSet is not considered ready: %w", err)
	}
	logger.Info("DaemonSet is ready", "name", ds.Name)

	return nil
}

// ensureDeleted is a noop. Deletion will be handled by ownerref
func (n topolvmNode) EnsureDeleted(_ Reconciler, _ context.Context, _ *lvmv1alpha1.LVMCluster) error {
	return nil
}

func getNodeDaemonSet(lvmCluster *lvmv1alpha1.LVMCluster, namespace string, args, csiArgs []string) *appsv1.DaemonSet {

	hostPathDirectory := corev1.HostPathDirectory
	hostPathDirectoryOrCreateType := corev1.HostPathDirectoryOrCreate

	volumes := []corev1.Volume{
		{Name: "registration-dir",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: fmt.Sprintf("%splugins_registry/", getAbsoluteKubeletPath(constants.CSIKubeletRootDir)),
					Type: &hostPathDirectory}}},
		{Name: "node-plugin-dir",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: fmt.Sprintf("%splugins/topolvm.io/node", getAbsoluteKubeletPath(constants.CSIKubeletRootDir)),
					Type: &hostPathDirectoryOrCreateType}}},
		{Name: "csi-plugin-dir",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: fmt.Sprintf("%splugins/kubernetes.io/csi", getAbsoluteKubeletPath(constants.CSIKubeletRootDir)),
					Type: &hostPathDirectoryOrCreateType}}},
		{Name: "pod-volumes-dir",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: fmt.Sprintf("%spods/", getAbsoluteKubeletPath(constants.CSIKubeletRootDir)),
					Type: &hostPathDirectoryOrCreateType}}},
		{Name: "lvmd-config-dir",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: filepath.Dir(lvmd.DefaultFileConfigPath),
					Type: &hostPathDirectory}}},
		{Name: "metrics-cert",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  topolvmMetricsSecretName,
					DefaultMode: ptr.To[int32](420),
				},
			}},
	}

	containers := []corev1.Container{
		*getNodeContainer(args),
		*getRBACProxyContainer(),
		*getCsiRegistrarContainer(csiArgs),
		*getNodeLivenessProbeContainer(),
	}

	// Affinity and tolerations
	nodeSelector, tolerations := selector.ExtractNodeSelectorAndTolerations(lvmCluster)

	topolvmNodeTolerations := []corev1.Toleration{}
	if tolerations != nil {
		topolvmNodeTolerations = tolerations
	}
	annotations := map[string]string{
		constants.WorkloadPartitioningManagementAnnotation: constants.ManagementAnnotationVal,
	}
	labels := map[string]string{
		constants.AppKubernetesNameLabel:      constants.CsiDriverNameVal,
		constants.AppKubernetesManagedByLabel: constants.ManagedByLabelVal,
		constants.AppKubernetesPartOfLabel:    constants.PartOfLabelVal,
		constants.AppKubernetesComponentLabel: constants.TopolvmNodeLabelVal,
	}
	nodeDaemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.TopolvmNodeDaemonsetName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        lvmCluster.Name,
					Annotations: annotations,
					Labels:      labels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: constants.TopolvmNodeServiceAccount,
					PriorityClassName:  constants.PriorityClassNameClusterCritical,
					Containers:         containers,
					Volumes:            volumes,
					HostPID:            true,
					Tolerations:        topolvmNodeTolerations,
				},
			},
		},
	}

	// set nodeSelector
	setDaemonsetNodeSelector(nodeSelector, nodeDaemonSet)

	return nodeDaemonSet
}

func getNodeContainer(args []string) *corev1.Container {
	command := []string{
		"/topolvm-node",
		"--embed-lvmd",
		fmt.Sprintf("--config=%s", lvmd.DefaultFileConfigPath),
	}

	command = append(command, args...)

	requirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(constants.TopolvmNodeCPURequest),
			corev1.ResourceMemory: resource.MustParse(constants.TopolvmNodeMemRequest),
		},
	}

	mountPropagationMode := corev1.MountPropagationBidirectional

	volumeMounts := []corev1.VolumeMount{
		{Name: "node-plugin-dir", MountPath: filepath.Dir(constants.DefaultCSISocket)},
		{Name: "lvmd-config-dir", MountPath: filepath.Dir(lvmd.DefaultFileConfigPath)},
		{Name: "pod-volumes-dir",
			MountPath:        fmt.Sprintf("%spods", getAbsoluteKubeletPath(constants.CSIKubeletRootDir)),
			MountPropagation: &mountPropagationMode},
		{Name: "csi-plugin-dir",
			MountPath:        fmt.Sprintf("%splugins/kubernetes.io/csi", getAbsoluteKubeletPath(constants.CSIKubeletRootDir)),
			MountPropagation: &mountPropagationMode},
	}

	env := []corev1.EnvVar{
		{Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName"}}}}

	node := &corev1.Container{
		Name:    constants.NodeContainerName,
		Image:   TopolvmCsiImage,
		Command: command,
		SecurityContext: &corev1.SecurityContext{
			Privileged: ptr.To(true),
			RunAsUser:  ptr.To(int64(0)),
		},
		Ports: []corev1.ContainerPort{{Name: constants.TopolvmNodeContainerHealthzName,
			ContainerPort: 9808,
			Protocol:      corev1.ProtocolTCP}},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{Path: "/healthz",
					Port: intstr.FromString(constants.TopolvmNodeContainerHealthzName)}},
			FailureThreshold:    3,
			InitialDelaySeconds: 10,
			TimeoutSeconds:      3,
			PeriodSeconds:       60},
		Resources:    requirements,
		Env:          env,
		VolumeMounts: volumeMounts,
	}
	return node
}

func getRBACProxyContainer() *corev1.Container {
	args := []string{
		"--secure-listen-address=0.0.0.0:8443",
		"--upstream=http://127.0.0.1:8080/",
		"--tls-cert-file=/var/run/secrets/serving-cert/tls.crt",
		"--tls-private-key-file=/var/run/secrets/serving-cert/tls.key",
		"--tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
	}

	requirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(constants.TopolvmNodeCPURequest),
			corev1.ResourceMemory: resource.MustParse(constants.TopolvmNodeMemRequest),
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "metrics-cert", ReadOnly: true, MountPath: "/var/run/secrets/serving-cert"},
	}

	node := &corev1.Container{
		Name:  "kube-rbac-proxy",
		Image: RbacProxyImage,
		Ports: []corev1.ContainerPort{
			{
				Name:          "https",
				ContainerPort: int32(8443),
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Args:         args,
		Resources:    requirements,
		VolumeMounts: volumeMounts,
	}
	return node
}

func getCsiRegistrarContainer(additionalArgs []string) *corev1.Container {
	args := []string{
		fmt.Sprintf("--csi-address=%s", constants.DefaultCSISocket),
		fmt.Sprintf("--kubelet-registration-path=%splugins/topolvm.io/node/csi-topolvm.sock", getAbsoluteKubeletPath(constants.CSIKubeletRootDir)),
	}
	args = append(args, additionalArgs...)

	volumeMounts := []corev1.VolumeMount{
		{Name: "node-plugin-dir", MountPath: filepath.Dir(constants.DefaultCSISocket)},
		{Name: "registration-dir", MountPath: "/registration"},
	}

	preStopCmd := []string{
		"/bin/sh",
		"-c",
		"rm -rf /registration/topolvm.io /registration/topolvm.io-reg.sock",
	}

	requirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(constants.CSIRegistrarCPURequest),
			corev1.ResourceMemory: resource.MustParse(constants.CSIRegistrarMemRequest),
		},
	}

	csiRegistrar := &corev1.Container{
		Name:         "csi-registrar",
		Image:        CsiRegistrarImage,
		Args:         args,
		Lifecycle:    &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: preStopCmd}}},
		VolumeMounts: volumeMounts,
		Resources:    requirements,
	}
	return csiRegistrar
}

func getNodeLivenessProbeContainer() *corev1.Container {
	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(constants.LivenessProbeCPURequest),
			corev1.ResourceMemory: resource.MustParse(constants.LivenessProbeMemRequest),
		},
	}

	args := []string{
		fmt.Sprintf("--csi-address=%s", constants.DefaultCSISocket),
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "node-plugin-dir", MountPath: filepath.Dir(constants.DefaultCSISocket)},
	}

	liveness := &corev1.Container{
		Name:         "liveness-probe",
		Image:        CsiLivenessProbeImage,
		Args:         args,
		VolumeMounts: volumeMounts,
		Resources:    resourceRequirements,
	}
	return liveness
}

func getAbsoluteKubeletPath(name string) string {
	if strings.HasSuffix(name, "/") {
		return name
	} else {
		return name + "/"
	}
}
