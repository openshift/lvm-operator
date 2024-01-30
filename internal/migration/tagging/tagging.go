package tagging

import (
	"context"
	"fmt"

	lvmv1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvm"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// AddTagToVGs adds a lvms tag to the existing volume groups. This is a temporary logic that should be removed in v4.16.
func AddTagToVGs(ctx context.Context, c client.Client, lvm lvm.LVM, nodeName string, namespace string) error {
	logger := log.FromContext(ctx)

	vgs, err := lvm.ListVGs()
	if err != nil {
		return fmt.Errorf("failed to list volume groups: %w", err)
	}

	lvmVolumeGroupList := &lvmv1.LVMVolumeGroupList{}
	err = c.List(ctx, lvmVolumeGroupList, &client.ListOptions{Namespace: namespace})
	if err != nil {
		return fmt.Errorf("failed to list LVMVolumeGroups: %w", err)
	}

	// If there is a matching LVMVolumeGroup CR, tag the existing volume group
	for _, vg := range vgs {
		tagged := false
		for _, lvmVolumeGroup := range lvmVolumeGroupList.Items {
			if vg.Name != lvmVolumeGroup.Name {
				continue
			}
			if lvmVolumeGroup.Spec.NodeSelector != nil {
				node := &corev1.Node{}
				err = c.Get(ctx, types.NamespacedName{Name: nodeName}, node)
				if err != nil {
					return fmt.Errorf("failed to get node %s: %w", nodeName, err)
				}

				matches, err := corev1helper.MatchNodeSelectorTerms(node, lvmVolumeGroup.Spec.NodeSelector)
				if err != nil {
					return fmt.Errorf("failed to match nodeSelector to node labels: %w", err)
				}
				if !matches {
					continue
				}
			}

			if err := lvm.AddTagToVG(vg.Name); err != nil {
				return err
			}
			tagged = true
		}
		if !tagged {
			logger.Info("skipping tagging volume group %s as there is no corresponding LVMVolumeGroup CR", vg.Name)
		}
	}

	return nil
}
