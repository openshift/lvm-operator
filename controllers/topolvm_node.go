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
	"context"
	"fmt"
	"path/filepath"
	"strings"

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
	topolvmNodeName = "topolvm-node"
)

type topolvmNode struct{}

func (n topolvmNode) getName() string {
	return topolvmNodeName
}

//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=create;update;delete;get;list;watch

func (n topolvmNode) ensureCreated(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	unitLogger := r.Log.WithValues("topolvmNode", n.getName())

	// get desired daemonSet spec
	dsTemplate := getNodeDaemonSet(lvmCluster, r.Namespace)
	// create desired daemonSet or update mutable fields on existing one
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dsTemplate.Name,
			Namespace: dsTemplate.Namespace,
		},
	}
	unitLogger.Info("running CreateOrUpdate")
	result, err := cutil.CreateOrUpdate(ctx, r.Client, ds, func() error {
		// at creation, deep copy the whole daemonSet
		if ds.CreationTimestamp.IsZero() {
			dsTemplate.DeepCopyInto(ds)
			return nil
		}
		// if update, update only mutable fields
		// For topolvm Node, we have containers, node selector and toleration terms

		// containers
		ds.Spec.Template.Spec.Containers = dsTemplate.Spec.Template.Spec.Containers

		// tolerations
		ds.Spec.Template.Spec.Tolerations = dsTemplate.Spec.Template.Spec.Tolerations

		// nodeSelector if non-nil
		if dsTemplate.Spec.Template.Spec.Affinity != nil {
			setDaemonsetNodeSelector(dsTemplate.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution, ds)
		}

		return nil
	})

	if err != nil {
		r.Log.Error(err, fmt.Sprintf("%s reconcile failure", topolvmNodeName), "name", ds.Name)
	} else {
		r.Log.Info(topolvmNodeName, "operation", result, "name", ds.Name)
	}
	return err
}

// ensureDeleted should wait for the resources to be cleaned up
func (n topolvmNode) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	NodeDaemonSet := &appsv1.DaemonSet{}
	err := r.Client.Get(ctx,
		types.NamespacedName{Name: TopolvmNodeDaemonsetName, Namespace: r.Namespace},
		NodeDaemonSet)

	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("topolvm node deleted", "TopolvmNode", NodeDaemonSet.Name)
			return nil
		}
		r.Log.Error(err, "failed to retrieve topolvm node daemonset", "TopolvmNode", NodeDaemonSet.Name)
		return err
	} else {
		// if not deleted, initiate deletion
		if NodeDaemonSet.GetDeletionTimestamp().IsZero() {
			if err = r.Client.Delete(ctx, NodeDaemonSet); err != nil {
				r.Log.Error(err, "failed to delete topolvm node daemonset", "TopolvmNodeName", TopolvmNodeDaemonsetName)
				return err
			} else {
				// set deletion in-progress for next reconcile to confirm deletion
				return fmt.Errorf("topolvm csi node daemonset %s is already marked for deletion", TopolvmNodeDaemonsetName)
			}
		}
	}

	return nil
}

// updateStatus should optionally update the CR's status about the health of the managed resource
// each unit will have updateStatus called individually so
// avoid status fields like lastHeartbeatTime and have a
// status that changes only when the operands change.
func (n topolvmNode) updateStatus(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	return nil
}

func getNodeDaemonSet(lvmCluster *lvmv1alpha1.LVMCluster, namespace string) *appsv1.DaemonSet {
	hostPathDirectory := corev1.HostPathDirectory
	hostPathDirectoryOrCreateType := corev1.HostPathDirectoryOrCreate
	storageMedium := corev1.StorageMediumMemory

	volumes := []corev1.Volume{
		{Name: "registration-dir",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: fmt.Sprintf("%splugins_registry/", getAbsoluteKubeletPath(CSIKubeletRootDir)),
					Type: &hostPathDirectory}}},
		{Name: "node-plugin-dir",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: fmt.Sprintf("%splugins/topolvm.cybozu.com/node", getAbsoluteKubeletPath(CSIKubeletRootDir)),
					Type: &hostPathDirectoryOrCreateType}}},
		{Name: "csi-plugin-dir",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: fmt.Sprintf("%splugins/kubernetes.io/csi", getAbsoluteKubeletPath(CSIKubeletRootDir)),
					Type: &hostPathDirectoryOrCreateType}}},
		{Name: "pod-volumes-dir",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: fmt.Sprintf("%spods/", getAbsoluteKubeletPath(CSIKubeletRootDir)),
					Type: &hostPathDirectoryOrCreateType}}},
		{Name: "lvmd-config-dir",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: filepath.Dir(LvmdConfigFile),
					Type: &hostPathDirectory}}},
		{Name: "lvmd-socket-dir",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{Medium: storageMedium}}},
	}

	initContainers := []corev1.Container{*getNodeInitContainer()}
	containers := []corev1.Container{*getLvmdContainer(), *getNodeContainer(), *getCsiRegistrarContainer(), *getNodeLivenessProbeContainer()}

	// Affinity and tolerations
	nodeSelector, tolerations := extractNodeSelectorAndTolerations(lvmCluster)

	topolvmNodeTolerations := []corev1.Toleration{}
	if tolerations != nil {
		topolvmNodeTolerations = tolerations
	}
	labels := map[string]string{
		"app": topolvmNodeName,
	}
	nodeDaemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TopolvmNodeDaemonsetName,
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
					Name:   lvmCluster.Name,
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: TopolvmNodeServiceAccount,
					InitContainers:     initContainers,
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

func getNodeInitContainer() *corev1.Container {
	command := []string{
		"sh",
		"-c",
		fmt.Sprintf("until [ -f %s ]; do echo waiting for lvmd config file; sleep 5; done", LvmdConfigFile),
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "lvmd-config-dir", MountPath: filepath.Dir(LvmdConfigFile)},
	}

	fileChecker := &corev1.Container{
		Name:         "file-checker",
		Image:        auxImage,
		Command:      command,
		VolumeMounts: volumeMounts,
	}

	return fileChecker
}

func getLvmdContainer() *corev1.Container {
	command := []string{
		"/lvmd",
		fmt.Sprintf("--config=%s", LvmdConfigFile),
		"--container=true",
	}

	resourceRequirements := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(TopolvmNodeCPULimit),
			corev1.ResourceMemory: resource.MustParse(TopolvmNodeMemLimit),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(TopolvmNodeCPURequest),
			corev1.ResourceMemory: resource.MustParse(TopolvmNodeMemRequest),
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "lvmd-socket-dir", MountPath: filepath.Dir(DefaultLVMdSocket)},
		{Name: "lvmd-config-dir", MountPath: filepath.Dir(LvmdConfigFile)},
	}

	privilege := true
	runUser := int64(0)
	lvmd := &corev1.Container{
		Name:  "lvmd",
		Image: TopolvmCsiImage,
		SecurityContext: &corev1.SecurityContext{
			Privileged: &privilege,
			RunAsUser:  &runUser,
		},
		Command:      command,
		Resources:    resourceRequirements,
		VolumeMounts: volumeMounts,
	}
	return lvmd
}

func getNodeContainer() *corev1.Container {
	privileged := true
	runAsUser := int64(0)

	command := []string{
		"/topolvm-node",
		fmt.Sprintf("--lvmd-socket=%s", DefaultLVMdSocket),
	}

	requirements := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(TopolvmNodeCPULimit),
			corev1.ResourceMemory: resource.MustParse(TopolvmNodeMemLimit),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(TopolvmNodeCPURequest),
			corev1.ResourceMemory: resource.MustParse(TopolvmNodeMemRequest),
		},
	}

	mountPropagationMode := corev1.MountPropagationBidirectional

	volumeMounts := []corev1.VolumeMount{
		{Name: "node-plugin-dir", MountPath: filepath.Dir(DefaultCSISocket)},
		{Name: "lvmd-socket-dir", MountPath: filepath.Dir(DefaultLVMdSocket)},
		{Name: "pod-volumes-dir",
			MountPath:        fmt.Sprintf("%spods", getAbsoluteKubeletPath(CSIKubeletRootDir)),
			MountPropagation: &mountPropagationMode},
		{Name: "csi-plugin-dir",
			MountPath:        fmt.Sprintf("%splugins/kubernetes.io/csi", getAbsoluteKubeletPath(CSIKubeletRootDir)),
			MountPropagation: &mountPropagationMode},
	}

	env := []corev1.EnvVar{
		{Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName"}}}}

	node := &corev1.Container{
		Name:    NodeContainerName,
		Image:   TopolvmCsiImage,
		Command: command,
		SecurityContext: &corev1.SecurityContext{
			Privileged: &privileged,
			RunAsUser:  &runAsUser,
		},
		Ports: []corev1.ContainerPort{{Name: TopolvmNodeContainerHealthzName,
			ContainerPort: 9808,
			Protocol:      corev1.ProtocolTCP}},
		LivenessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{Path: "/healthz",
					Port: intstr.FromString(TopolvmNodeContainerHealthzName)}},
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

func getCsiRegistrarContainer() *corev1.Container {
	command := []string{
		"/csi-node-driver-registrar",
		fmt.Sprintf("--csi-address=%s", DefaultCSISocket),
		fmt.Sprintf("--kubelet-registration-path=%splugins/topolvm.cybozu.com/node/csi-topolvm.sock", getAbsoluteKubeletPath(CSIKubeletRootDir)),
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "node-plugin-dir", MountPath: filepath.Dir(DefaultCSISocket)},
		{Name: "registration-dir", MountPath: "/registration"},
	}

	preStopCmd := []string{
		"/bin/sh",
		"-c",
		"rm -rf /registration/topolvm.cybozu.com /registration/topolvm.cybozu.com-reg.sock",
	}

	csiRegistrar := &corev1.Container{
		Name:         "csi-registrar",
		Image:        CsiRegistrarImage,
		Command:      command,
		Lifecycle:    &corev1.Lifecycle{PreStop: &corev1.Handler{Exec: &corev1.ExecAction{Command: preStopCmd}}},
		VolumeMounts: volumeMounts,
	}
	return csiRegistrar
}

func getNodeLivenessProbeContainer() *corev1.Container {
	command := []string{
		"/livenessprobe",
		fmt.Sprintf("--csi-address=%s", DefaultCSISocket),
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "node-plugin-dir", MountPath: filepath.Dir(DefaultCSISocket)},
	}

	liveness := &corev1.Container{
		Name:         "liveness-probe",
		Image:        CsiLivenessProbeImage,
		Command:      command,
		VolumeMounts: volumeMounts,
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
