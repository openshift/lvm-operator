package tagging

import (
	"context"
	"errors"
	"fmt"

	lvmv1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// AddTagToVGs adds a lvms tag to the existing volume groups. This is a temporary logic that should be removed in v4.16.
func AddTagToVGs(ctx context.Context, c client.Client, lvm lvm.LVM, nodeName string, namespace string) error {
	logger := log.FromContext(ctx)

	// now we get all untagged vgs on the node
	vgs, err := lvm.ListVGs(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to list volume groups on node "+
			"to determine if tag migration is necessary: %w", err)
	} else if len(vgs) == 0 {
		logger.Info("no untagged volume groups found on the node, skipping migration")
		return nil
	}
	logger.Info("found untagged volume groups on the node", "vgs", vgs)

	potentialVGsInNode, err := vgCandidatesForTagMigration(ctx, c, nodeName, namespace)
	if err != nil {
		return fmt.Errorf("failed to get potential volume groups for node during tag migration: %w", err)
	} else if len(potentialVGsInNode) == 0 {
		logger.Info("no potential volume groups found for the node, skipping migration")
		return nil
	}
	logger.Info("found existing LVMVolumeGroups candidates for the node", "vgs", potentialVGsInNode)

	var tagged []string
	var taggingErr error

	for _, potentialVG := range potentialVGsInNode {
		// If there is an existing Volume Group managed by LVMS (through LVMVolumeGroup) that is also present on the node
		// but is not tagged, we should tag it now.
		for _, vg := range vgs {
			if vg.Name != potentialVG {
				continue
			}
			logger.Info("tagging volume group managed by LVMS", "vg", vg.Name)
			if err := lvm.AddTagToVG(ctx, vg.Name); err != nil {
				taggingErr = errors.Join(taggingErr, fmt.Errorf("failed to tag volume group %s: %w", vg.Name, err))
				continue
			}
			tagged = append(tagged, vg.Name)
		}
	}

	if len(tagged) > 0 {
		logger.Info("tagged volume groups", "count", len(tagged), "vgs", tagged)
	} else {
		logger.Info("no volume groups were tagged")
	}

	if taggingErr != nil {
		return fmt.Errorf("failed to tag at least one volume group: %w", taggingErr)
	}

	return nil
}

func vgCandidatesForTagMigration(ctx context.Context, c client.Client, nodeName, namespace string) ([]string, error) {
	node := &corev1.Node{}
	if err := c.Get(ctx, types.NamespacedName{Name: nodeName}, node); err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}

	lvmVolumeGroupList := &lvmv1.LVMVolumeGroupList{}
	if err := c.List(ctx, lvmVolumeGroupList, &client.ListOptions{Namespace: namespace}); err != nil {
		return nil, fmt.Errorf("failed to list LVMVolumeGroups from cluster: %w", err)
	}

	// to tag only the volume groups that are not tagged yet and managed by the operator
	// we need to find the volume groups that should be on the node already (based on the LVMVolumeGroup CRs)
	// and then reapply the tag to them if they are not tagged yet.
	// however, even though the CR exists it might not be on the node yet, so its only a candidate for tagging
	var potentialVGsInNode []string
	for _, lvmVolumeGroup := range lvmVolumeGroupList.Items {
		// If the LVMVolumeGroup CR does not have a nodeSelector, the vg is a potential match by default
		if lvmVolumeGroup.Spec.NodeSelector == nil {
			potentialVGsInNode = append(potentialVGsInNode, lvmVolumeGroup.GetName())
			continue
		}
		// If the LVMVolumeGroup CR has a nodeSelector, the vg is a potential match if the node labels match the nodeSelector
		matches, err := corev1helper.MatchNodeSelectorTerms(node, lvmVolumeGroup.Spec.NodeSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to match nodeSelector to node labels: %w", err)
		}
		if matches {
			potentialVGsInNode = append(potentialVGsInNode, lvmVolumeGroup.GetName())
		}
	}
	return potentialVGsInNode, nil
}
