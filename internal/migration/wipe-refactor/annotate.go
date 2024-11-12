package wipe_refactor

import (
	"context"
	"fmt"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"time"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type WipeRefactor struct {
	namespace string
	client    client.Client
}

func NewWipeRefactor(client client.Client, namespace string) *WipeRefactor {
	return &WipeRefactor{
		namespace: namespace,
		client:    client,
	}
}

func (w *WipeRefactor) AnnotateExistingLVMVolumeGroupsIfWipingEnabled(ctx context.Context) error {
	logger := log.FromContext(ctx)
	nodeStatusList := &lvmv1alpha1.LVMVolumeGroupNodeStatusList{}
	if err := w.client.List(ctx, nodeStatusList, &client.ListOptions{Namespace: w.namespace}); err != nil {
		return fmt.Errorf("failed to list LVMVolumeGroupNodeStatus instances: %w", err)
	}
	if len(nodeStatusList.Items) < 1 {
		return nil
	}

	lvmVolumeGroupList := &lvmv1alpha1.LVMVolumeGroupList{}
	if err := w.client.List(ctx, lvmVolumeGroupList, &client.ListOptions{Namespace: w.namespace}); err != nil {
		return fmt.Errorf("failed to list LVMVolumeGroup instances: %w", err)
	}
	if len(lvmVolumeGroupList.Items) < 1 {
		return nil
	}

	for _, volumeGroup := range lvmVolumeGroupList.Items {
		updated := false
		for _, nodeStatus := range nodeStatusList.Items {
			for _, vgStatus := range nodeStatus.Spec.LVMVGStatus {
				if vgStatus.Name == volumeGroup.Name {
					if volumeGroup.Spec.DeviceSelector == nil || volumeGroup.Spec.DeviceSelector.ForceWipeDevicesAndDestroyAllData == nil || !*volumeGroup.Spec.DeviceSelector.ForceWipeDevicesAndDestroyAllData {
						continue
					}
					if volumeGroup.Annotations == nil {
						volumeGroup.Annotations = make(map[string]string)
					}
					volumeGroup.Annotations[constants.DevicesWipedAnnotationPrefix+nodeStatus.GetName()] = fmt.Sprintf(
						"the devices of this volume group have been wiped at %s by lvms according to policy. This marker"+
							"serves as indicator that the devices have been wiped before and should not be wiped again."+
							"removal of this annotation is unsupported and may lead to data loss due to additional wiping.",
						time.Now().Format(time.RFC3339))
					updated = true
				}
			}
		}
		if updated {
			if err := w.client.Update(ctx, &volumeGroup); err != nil {
				return fmt.Errorf("failed to add wiped annotation to LVMVolumeGroup instance: %w", err)
			}
			logger.Info("successfully added wiped annotation to LVMVolumeGroup instance", "LVMVolumeGroupName", volumeGroup.GetName())
		}
	}

	return nil
}
