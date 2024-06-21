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

	secv1 "github.com/openshift/api/security/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/labels"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	sccName = "topolvm-scc"
)

func OpenShiftSCCs() Manager {
	return openshiftSccs{}
}

type openshiftSccs struct{}

// openshiftSccs unit satisfies resourceManager interface
var _ Manager = openshiftSccs{}

func (c openshiftSccs) GetName() string {
	return sccName
}

//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;update;delete;patch

func (c openshiftSccs) EnsureCreated(r Reconciler, ctx context.Context, cluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", c.GetName())
	sccs := getAllSCCs(r.GetNamespace())
	for _, template := range sccs {
		scc := &secv1.SecurityContextConstraints{
			ObjectMeta: metav1.ObjectMeta{
				Name: template.Name,
			},
		}

		result, err := cutil.CreateOrUpdate(ctx, r, scc, func() error {
			if scc.CreationTimestamp.IsZero() {
				template.DeepCopyInto(scc)
			}
			labels.SetManagedLabels(r.Scheme(), scc, cluster)
			scc.Users = template.Users
			return nil
		})
		if err != nil {
			return fmt.Errorf("%s failed to reconcile: %w", c.GetName(), err)
		}
		if result != cutil.OperationResultNone {
			logger.V(2).Info("SecurityContextConstraint applied to cluster", "operation", result, "name", scc.Name)
		}
	}

	return nil
}

func (c openshiftSccs) EnsureDeleted(r Reconciler, ctx context.Context, _ *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", c.GetName())
	sccs := getAllSCCs(r.GetNamespace())
	for _, scc := range sccs {
		name := types.NamespacedName{Name: scName}
		logger := logger.WithValues("SecurityContextConstraint", scName)
		if err := r.Get(ctx, name, scc); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}

		if !scc.GetDeletionTimestamp().IsZero() {
			return fmt.Errorf("the SecurityContextConstraint %s is still present, waiting for deletion", scName)
		}

		if err := r.Delete(ctx, scc); err != nil {
			return fmt.Errorf("failed to delete SecurityContextConstraint %s: %w", scc.GetName(), err)
		}
		logger.Info("initiated SecurityContextConstraint deletion")
	}
	return nil
}

func getAllSCCs(namespace string) []*secv1.SecurityContextConstraints {
	return []*secv1.SecurityContextConstraints{
		newVGManagerScc(namespace),
	}
}

func newVGManagerScc(namespace string) *secv1.SecurityContextConstraints {
	scc := &secv1.SecurityContextConstraints{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "security.openshift.io/v1",
			Kind:       "SecurityContextConstraints",
		},
	}
	scc.Name = constants.SCCPrefix + "vgmanager"
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
		fmt.Sprintf("system:serviceaccount:%s:%s", namespace, constants.VGManagerServiceAccount),
	}

	return scc
}
