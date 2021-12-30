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

	secv1 "github.com/openshift/api/security/v1"
	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	sccName = "topolvm-scc"
)

type openshiftSccs struct{}

// openshiftSccs unit satisfies resourceManager interface
var _ resourceManager = openshiftSccs{}

func (c openshiftSccs) getName() string {
	return sccName
}

func (c openshiftSccs) ensureCreated(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	if !IsOpenshift(r) {
		r.Log.Info("not creating SCCs as this is not an Openshift cluster")
		return nil
	}
	sccs := getAllSCCs(lvmCluster.Namespace)
	for _, scc := range sccs {
		_, err := r.SecurityClient.SecurityContextConstraints().Get(ctx, scc.Name, metav1.GetOptions{})
		if err != nil && errors.IsNotFound(err) {
			r.Log.Info("creating SecurityContextConstraint", "SecurityContextConstraint", scc.Name)
			_, err := r.SecurityClient.SecurityContextConstraints().Create(ctx, scc, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create SCC %q: %v", scc.Name, err)
			}
		} else if err == nil {
			// Don't update the SCC
			r.Log.Info("already exists", "SecurityContextConstraint", scc.Name)
		} else {
			r.Log.Error(err, "Something went wrong when checking for SecurityContextConstraint", "SecurityContextConstraint", scc.Name)
			return fmt.Errorf("something went wrong when checking for SCC %q: %v", scc.Name, err)
		}
	}

	return nil
}

func (c openshiftSccs) ensureDeleted(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	if IsOpenshift(r) {
		var err error
		sccs := getAllSCCs(lvmCluster.Namespace)
		for _, scc := range sccs {
			err = r.SecurityClient.SecurityContextConstraints().Delete(ctx, scc.Name, metav1.DeleteOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					r.Log.Info("SecurityContextConstraint is already deleted", "SecurityContextConstraint", scc.Name)
					return nil
				} else {
					r.Log.Error(err, "failed to delete SecurityContextConstraint", "SecurityContextConstraint", scc.Name)
				}
			}
		}
	}
	return nil
}

func (c openshiftSccs) updateStatus(r *LVMClusterReconciler, ctx context.Context, lvmCluster *lvmv1alpha1.LVMCluster) error {
	// intentionally empty
	return nil
}

func getAllSCCs(namespace string) []*secv1.SecurityContextConstraints {
	return []*secv1.SecurityContextConstraints{
		newTopolvmNodeScc(namespace),
		newVGManagerScc(namespace),
	}
}

func newTopolvmNodeScc(namespace string) *secv1.SecurityContextConstraints {
	scc := &secv1.SecurityContextConstraints{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "security.openshift.io/v1",
			Kind:       "SecurityContextConstraints",
		},
	}
	scc.Name = "odf-lvm-topolvm-node"
	scc.AllowPrivilegedContainer = true
	scc.AllowHostNetwork = false
	scc.AllowHostDirVolumePlugin = true
	scc.AllowHostPorts = false
	scc.AllowHostPID = true
	scc.AllowHostIPC = false
	scc.ReadOnlyRootFilesystem = false
	scc.RequiredDropCapabilities = []corev1.Capability{}
	scc.RunAsUser = secv1.RunAsUserStrategyOptions{
		Type: secv1.RunAsUserStrategyRunAsAny,
	}
	scc.SELinuxContext = secv1.SELinuxContextStrategyOptions{
		Type: secv1.SELinuxStrategyRunAsAny,
	}
	scc.FSGroup = secv1.FSGroupStrategyOptions{
		Type: secv1.FSGroupStrategyRunAsAny,
	}
	scc.SupplementalGroups = secv1.SupplementalGroupsStrategyOptions{
		Type: secv1.SupplementalGroupsStrategyRunAsAny,
	}
	scc.Volumes = []secv1.FSType{
		secv1.FSTypeConfigMap,
		secv1.FSTypeEmptyDir,
		secv1.FSTypeHostPath,
		secv1.FSTypeSecret,
	}
	scc.Users = []string{
		fmt.Sprintf("system:serviceaccount:%s:%s", namespace, TopolvmNodeServiceAccount),
	}

	return scc
}

func newVGManagerScc(namespace string) *secv1.SecurityContextConstraints {
	scc := &secv1.SecurityContextConstraints{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "security.openshift.io/v1",
			Kind:       "SecurityContextConstraints",
		},
	}
	scc.Name = "odf-lvm-vgmanager"
	scc.AllowPrivilegedContainer = true
	scc.AllowHostNetwork = false
	scc.AllowHostDirVolumePlugin = true
	scc.AllowHostPorts = false
	scc.AllowHostPID = true
	scc.AllowHostIPC = true
	scc.ReadOnlyRootFilesystem = false
	scc.RequiredDropCapabilities = []corev1.Capability{}
	scc.RunAsUser = secv1.RunAsUserStrategyOptions{
		Type: secv1.RunAsUserStrategyRunAsAny,
	}
	scc.SELinuxContext = secv1.SELinuxContextStrategyOptions{
		Type: secv1.SELinuxStrategyMustRunAs,
	}
	scc.FSGroup = secv1.FSGroupStrategyOptions{
		Type: secv1.FSGroupStrategyMustRunAs,
	}
	scc.SupplementalGroups = secv1.SupplementalGroupsStrategyOptions{
		Type: secv1.SupplementalGroupsStrategyRunAsAny,
	}
	scc.Volumes = []secv1.FSType{
		secv1.FSTypeConfigMap,
		secv1.FSTypeEmptyDir,
		secv1.FSTypeHostPath,
		secv1.FSTypeSecret,
	}
	scc.Users = []string{
		fmt.Sprintf("system:serviceaccount:%s:%s", namespace, VGManagerServiceAccount),
	}

	return scc
}
