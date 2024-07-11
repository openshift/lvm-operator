package resource

import (
	"context"
	"fmt"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/selector"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	resourceName = "topolvm-csi-node-registrations"
)

func CSINode() Manager {
	return csiNode{}
}

type csiNode struct {
}

func (c csiNode) GetName() string {
	return resourceName
}

func (c csiNode) EnsureCreated(r Reconciler, ctx context.Context, cluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", c.GetName())

	csiNodes, err := c.GetAllCSINodeCandidates(ctx, r, cluster)
	if err != nil {
		return err
	}

	for _, csiNode := range csiNodes {
		found := false
		for _, driver := range csiNode.Spec.Drivers {
			if driver.Name == constants.TopolvmCSIDriverName {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("csi node %s does not have driver %s", csiNode.Name, constants.TopolvmCSIDriverName)
		}
	}

	logger.V(2).Info("All CSINode driver registrations have been created by the kubelet")

	return nil
}

func (c csiNode) EnsureDeleted(r Reconciler, ctx context.Context, cluster *lvmv1alpha1.LVMCluster) error {
	logger := log.FromContext(ctx).WithValues("resourceManager", c.GetName())

	csiNodes, err := c.GetAllCSINodeCandidates(ctx, r, cluster)
	if err != nil {
		return err
	}

	for _, csiNode := range csiNodes {
		found := false
		for _, driver := range csiNode.Spec.Drivers {
			if driver.Name == constants.TopolvmCSIDriverName {
				found = true
				break
			}
		}
		if found {
			return fmt.Errorf("csi node %s does not have driver %s", csiNode.Name, constants.TopolvmCSIDriverName)
		}
	}

	logger.V(2).Info("All CSINode driver registrations have been deleted by the kubelet")

	return nil
}

func (c csiNode) GetAllCSINodeCandidates(ctx context.Context, clnt client.Client, cluster *lvmv1alpha1.LVMCluster) ([]*storagev1.CSINode, error) {
	nodeList := &v1.NodeList{}
	if err := clnt.List(ctx, nodeList); err != nil {
		return nil, err
	}

	valid, err := selector.ValidNodes(cluster, nodeList)
	if err != nil {
		return nil, err
	}

	csiNodes := make([]*storagev1.CSINode, 0, len(valid))
	for _, node := range valid {
		csiNode := &storagev1.CSINode{
			ObjectMeta: metav1.ObjectMeta{
				Name: node.Name,
			},
		}
		if err := clnt.Get(ctx, client.ObjectKeyFromObject(csiNode), csiNode); err != nil {
			return nil, err
		}
		csiNodes = append(csiNodes, csiNode)

	}

	return csiNodes, nil
}

var _ Manager = csiNode{}
