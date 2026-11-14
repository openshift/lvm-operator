/*
Copyright © 2026 Red Hat, Inc.

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

package vgmanager

import (
	"context"
	"testing"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// A disk that vanished was only noticed when a removal happened to be pending, so a
// volume group kept reporting Ready while LVM already recorded a missing physical
// volume and the node had silently lost that capacity.
func TestReadyStatusReportsMissingPhysicalVolumes(t *testing.T) {
	for _, tc := range []struct {
		name string
		pvs  []lvm.PhysicalVolume
		want v1alpha1.VGStatusType
	}{
		{
			name: "every device is present",
			pvs:  []lvm.PhysicalVolume{{PvName: "/dev/sda"}, {PvName: "/dev/sdb"}},
			want: v1alpha1.VGStatusReady,
		},
		{
			name: "a device is missing",
			pvs:  []lvm.PhysicalVolume{{PvName: "/dev/sda"}, {PvName: "[unknown]", PvMissing: "missing"}},
			want: v1alpha1.VGStatusDegraded,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			require.NoError(t, v1alpha1.AddToScheme(scheme))

			vg := &v1alpha1.LVMVolumeGroup{}
			vg.Name, vg.Namespace = "vg1", "lvms"
			r := &Reconciler{
				Client:    fake.NewClientBuilder().WithScheme(scheme).Build(),
				Scheme:    scheme,
				NodeName:  "node-a",
				Namespace: "lvms",
			}

			_, degraded, err := r.setVolumeGroupReadyStatus(ctx,
				vg, []lvm.VolumeGroup{{Name: "vg1", PVs: tc.pvs}}, FilteredBlockDevices{})
			require.NoError(t, err)
			// The caller announces VolumeGroupReady off this flag, so it must not say
			// everything is attached on the pass that just reported a missing device.
			require.Equal(t, tc.want == v1alpha1.VGStatusDegraded, degraded)

			nodeStatus := &v1alpha1.LVMVolumeGroupNodeStatus{}
			require.NoError(t, r.Get(ctx, client.ObjectKey{Name: "node-a", Namespace: "lvms"}, nodeStatus))
			require.Len(t, nodeStatus.Spec.LVMVGStatus, 1)
			require.Equal(t, tc.want, nodeStatus.Spec.LVMVGStatus[0].Status)

			if tc.want == v1alpha1.VGStatusDegraded {
				require.Contains(t, nodeStatus.Spec.LVMVGStatus[0].Reason, "missing one or more physical volumes")
				require.Contains(t, nodeStatus.Spec.LVMVGStatus[0].Reason, "node-a")
			} else {
				require.Empty(t, nodeStatus.Spec.LVMVGStatus[0].Reason)
			}
		})
	}
}
