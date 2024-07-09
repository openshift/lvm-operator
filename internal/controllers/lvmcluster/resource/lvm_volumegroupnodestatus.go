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

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/selector"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	lvmVGNodeStatusName       = "lvmvgnodestatus"
	deleteProtectionFinalizer = "delete-protection.lvm.openshift.io"
)

func LVMVGNodeStatus() Manager {
	return lvmVGNodeStatus{}
}

type lvmVGNodeStatus struct{}

var _ Manager = lvmVGNodeStatus{}

func (l lvmVGNodeStatus) GetName() string {
	return lvmVGNodeStatusName
}

func (l lvmVGNodeStatus) EnsureCreated(r Reconciler, ctx context.Context, cluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", l.GetName())

	nodes := v1.NodeList{}
	if err := r.List(ctx, &nodes); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	validNodes, err := selector.ValidNodes(cluster, &nodes)
	if err != nil {
		return fmt.Errorf("failed to get valid nodes: %w", err)
	}

	logger.V(1).Info("nodes considered for LVMCluster",
		"nodes", nodesToStringSummary(validNodes),
		"total", nodesToStringSummary(nodes.Items),
	)

	for _, node := range validNodes {
		lvmVGNodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{
			ObjectMeta: metav1.ObjectMeta{
				Name:      node.Name,
				Namespace: r.GetNamespace(),
			},
		}
		result, err := cutil.CreateOrUpdate(ctx, r, lvmVGNodeStatus, func() error {
			if err := ctrl.SetControllerReference(cluster, lvmVGNodeStatus, r.Scheme()); err != nil {
				return fmt.Errorf("failed to set controller reference: %w", err)
			}
			if !hasDeleteProtectionFinalizer(lvmVGNodeStatus.Finalizers) {
				lvmVGNodeStatus.SetFinalizers(append(lvmVGNodeStatus.Finalizers, deleteProtectionFinalizer))
			}
			return nil
		})

		if err != nil {
			return fmt.Errorf("%s failed to reconcile: %w", l.GetName(), err)
		}

		if result != cutil.OperationResultNone {
			logger.V(1).Info("LVMVolumeGroupNodeStatus applied to cluster", "operation", result, "name", lvmVGNodeStatus.Name)
		}
	}

	return nil
}

func (l lvmVGNodeStatus) EnsureDeleted(r Reconciler, ctx context.Context, cluster *lvmv1alpha1.LVMCluster) error {

	nodeStatusList := &lvmv1alpha1.LVMVolumeGroupNodeStatusList{}
	if err := r.List(ctx, nodeStatusList, client.InNamespace(r.GetNamespace())); err != nil {
		return fmt.Errorf("failed to list LVMVolumeGroupNodeStatus: %w", err)
	}

	nodeList := v1.NodeList{}
	if err := r.List(ctx, &nodeList); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	validNodes, err := selector.ValidNodes(cluster, &nodeList)
	if err != nil {
		return fmt.Errorf("failed to get valid nodes: %w", err)
	}

	for _, status := range nodeStatusList.Items {
		if isValidNode(status.Name, validNodes) {
			if err := l.deleteNodeStatus(r, ctx, status); err != nil {
				return err
			}
		}
	}
	return nil
}

// isValidNode checks if the node is in the list of valid nodes for the LVMVolumeGroupNodeStatus.
func isValidNode(statusName string, validNodes []v1.Node) bool {
	for _, node := range validNodes {
		if statusName == node.Name {
			return true
		}
	}
	return false
}

// deleteNodeStatus deletes the LVMVolumeGroupNodeStatus if it is not marked for deletion and removes the finalizer.
func (l lvmVGNodeStatus) deleteNodeStatus(r Reconciler, ctx context.Context, status lvmv1alpha1.LVMVolumeGroupNodeStatus) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", l.GetName())

	if status.GetDeletionTimestamp().IsZero() {
		if err := r.Delete(ctx, &status); err != nil {
			return fmt.Errorf("failed to delete LVMVolumeGroupNodeStatus %s: %w", status.GetName(), err)
		}
		logger.Info("initiated LVMVolumeGroupNodeStatus deletion", "nodeStatus", client.ObjectKeyFromObject(&status))
	}

	if removeDeleteProtectionFinalizer(&status) {
		if err := r.Update(ctx, &status); err != nil {
			return fmt.Errorf("failed to remove finalizer from LVMVolumeGroupNodeStatus: %w", err)
		}
	}
	return nil
}

func removeDeleteProtectionFinalizer(status *lvmv1alpha1.LVMVolumeGroupNodeStatus) bool {
	finalizers := status.GetFinalizers()
	for i, finalizer := range finalizers {
		if finalizer == deleteProtectionFinalizer {
			status.SetFinalizers(append(finalizers[:i], finalizers[i+1:]...))
			return true
		}
	}
	return false
}

func hasDeleteProtectionFinalizer(finalizers []string) bool {
	for _, f := range finalizers {
		if f == deleteProtectionFinalizer {
			return true
		}
	}
	return false
}

// nodesToStringSummary returns a string representation of the node names for logging,
// as it is too long to log all nodes as objects.
func nodesToStringSummary(nodes []v1.Node) string {
	nodeNames := make([]string, len(nodes))
	for i, node := range nodes {
		nodeNames[i] = node.Name
	}
	return fmt.Sprintf("%v", nodeNames)
}
