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
	"errors"
	"testing"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	lvmmocks "github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm/mocks"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGlobMandatoryDevices(t *testing.T) {
	for _, tt := range []struct {
		name      string
		devices   FilteredBlockDevices
		paths     []v1alpha1.DevicePath
		wantError string
	}{
		{name: "multiple matching devices", devices: FilteredBlockDevices{Available: []lsblk.BlockDevice{{KName: "/dev/sdb"}, {KName: "/dev/sdc"}}}, paths: []v1alpha1.DevicePath{"glob:/dev/sd[b-c]"}},
		{name: "one matching device already in VG", devices: FilteredBlockDevices{Available: []lsblk.BlockDevice{{KName: "/dev/sdb"}}, Excluded: []FilteredBlockDevice{{BlockDevice: lsblk.BlockDevice{KName: "/dev/sdc"}, FilterErrors: []error{filter.ErrDeviceAlreadySetupCorrectly}}}}, paths: []v1alpha1.DevicePath{"glob:/dev/sd[b-c]"}},
		{name: "every required pattern must match", devices: FilteredBlockDevices{Available: []lsblk.BlockDevice{{KName: "/dev/sdb"}}}, paths: []v1alpha1.DevicePath{"glob:/dev/sdb", "glob:/dev/nvme*"}, wantError: "did not match any"},
		{name: "partition is not a full device match", devices: FilteredBlockDevices{Available: []lsblk.BlockDevice{{KName: "/dev/sdb1"}}}, paths: []v1alpha1.DevicePath{"glob:/dev/sdb"}, wantError: "did not match any"},
		{name: "one unsafe match fails a required pattern", devices: FilteredBlockDevices{Available: []lsblk.BlockDevice{{KName: "/dev/sdb"}}, Excluded: []FilteredBlockDevice{{BlockDevice: lsblk.BlockDevice{KName: "/dev/sdc"}, FilterErrors: []error{errors.New("read-only")}}}}, paths: []v1alpha1.DevicePath{"glob:/dev/sd[b-c]"}, wantError: "read-only"},
		{name: "invalid pattern", paths: []v1alpha1.DevicePath{"glob:/dev/["}, wantError: "invalid device path pattern"},
		{
			// "glob:/dev/nvme2*" matches the disk and its own partition. A disk with
			// children is never a valid PV, so only the partition is verified — otherwise
			// the parent's "has children" exclusion would fail an otherwise fine selector.
			name: "a matched partition shadows its parent disk",
			devices: FilteredBlockDevices{
				Available: []lsblk.BlockDevice{{KName: "/dev/nvme2n1p1"}},
				Excluded: []FilteredBlockDevice{{
					BlockDevice:  lsblk.BlockDevice{KName: "/dev/nvme2n1", Children: []lsblk.BlockDevice{{KName: "/dev/nvme2n1p1"}}},
					FilterErrors: []error{errors.New("has children")},
				}},
			},
			paths: []v1alpha1.DevicePath{"glob:/dev/nvme2*"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resolver := symlinkResolver.NewWithResolver(func(string) (string, error) { t.Fatal("glob selector must not resolve symlinks"); return "", nil })
			err := VerifyMandatoryDevicePaths(tt.devices, resolver, tt.paths)
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
		})
	}
}

func TestGlobDeviceRemoval(t *testing.T) {
	for _, tt := range []struct {
		name            string
		paths, optional []v1alpha1.DevicePath
		pvs             []string
		aliases         map[string]string
		remove          []string
		wantError       string
		raid            *v1alpha1.RAIDConfig
	}{
		{name: "retain all required matches", paths: []v1alpha1.DevicePath{"glob:/dev/sd[b-c]"}, pvs: []string{"/dev/sdb", "/dev/sdc"}},
		{name: "narrowing a required pattern removes only unmatched PV", paths: []v1alpha1.DevicePath{"glob:/dev/sdb"}, pvs: []string{"/dev/sdb", "/dev/sdc"}, remove: []string{"/dev/sdc"}},
		{name: "optional pattern retains its matches", paths: []v1alpha1.DevicePath{"/dev/sdb"}, optional: []v1alpha1.DevicePath{"glob:/dev/sd[c-d]", "glob:/dev/nvme*"}, pvs: []string{"/dev/sdb", "/dev/sdc"}},
		{name: "narrowed optional pattern removes a PV", optional: []v1alpha1.DevicePath{"glob:/dev/sdb"}, pvs: []string{"/dev/sdb", "/dev/sdc"}, remove: []string{"/dev/sdc"}},
		{name: "missing optional pattern does not block removal", paths: []v1alpha1.DevicePath{"/dev/sdb"}, optional: []v1alpha1.DevicePath{"glob:/dev/nvme*"}, pvs: []string{"/dev/sdb", "/dev/sdc"}, remove: []string{"/dev/sdc"}},
		{name: "match kernel name behind PV alias", paths: []v1alpha1.DevicePath{"glob:/dev/dm-*"}, pvs: []string{"/dev/mapper/crypt"}, aliases: map[string]string{"/dev/mapper/crypt": "/dev/dm-0"}},
		{name: "literal symlink still retains PV", paths: []v1alpha1.DevicePath{"/dev/disk/by-id/data", "glob:/dev/sdc"}, pvs: []string{"/dev/sdb", "/dev/sdc"}, aliases: map[string]string{"/dev/disk/by-id/data": "/dev/sdb"}},
		{name: "unresolvable alias blocks removal", optional: []v1alpha1.DevicePath{"glob:/dev/sdb"}, pvs: []string{"/dev/sdb", "/dev/mapper/broken"}, wantError: "failed to resolve PV"},
		// Only PVs no pattern already matches are resolved, so a flaky lookup on a PV the
		// pattern plainly covers cannot fail the reconcile.
		{name: "a PV the pattern already matches needs no symlink lookup", paths: []v1alpha1.DevicePath{"glob:/dev/mapper/*"}, pvs: []string{"/dev/mapper/broken", "/dev/sdb"}, remove: []string{"/dev/sdb"}},
		{name: "invalid optional pattern blocks removal", paths: []v1alpha1.DevicePath{"/dev/sdb"}, optional: []v1alpha1.DevicePath{"glob:/dev/["}, pvs: []string{"/dev/sdb", "/dev/sdc"}, wantError: "invalid device path pattern"},
		// Swapping disks: the new pattern matches no current PV because the device it
		// selects is not in the VG yet. Removal of the old PVs proceeds; "must match at
		// least one device" is enforced against the host, not against the current PVs.
		{name: "a required pattern matching no current PV still removes the old ones", paths: []v1alpha1.DevicePath{"glob:/dev/sdb", "glob:/dev/nvme0*"}, pvs: []string{"/dev/sdb", "/dev/sdc"}, remove: []string{"/dev/sdc"}},
		{name: "removing every PV is still refused", paths: []v1alpha1.DevicePath{"glob:/dev/nvme*"}, pvs: []string{"/dev/sdb", "/dev/sdc"}, wantError: "less than 1 device"},
		{name: "cannot remove last PV", optional: []v1alpha1.DevicePath{"glob:/dev/nvme*"}, pvs: []string{"/dev/sdb"}, wantError: "less than 1 device"},
		{name: "RAID minimum is still enforced", optional: []v1alpha1.DevicePath{"glob:/dev/sdb"}, pvs: []string{"/dev/sdb", "/dev/sdc"}, wantError: "requires at least 2 devices", raid: &v1alpha1.RAIDConfig{Type: v1alpha1.RAIDTypeRAID1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mockLVM := lvmmocks.NewMockLVM(t)
			for _, device := range tt.remove {
				mockLVM.EXPECT().ReduceVG(ctx, "vg1", device).Return(nil).Once()
				mockLVM.EXPECT().RemovePV(ctx, device).Return(nil).Once()
			}
			scheme := runtime.NewScheme()
			require.NoError(t, v1alpha1.AddToScheme(scheme))
			r := &Reconciler{LVM: mockLVM, EventRecorder: events.NewFakeRecorder(10), Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
			vg := &v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{Name: "vg1"}, Spec: v1alpha1.LVMVolumeGroupSpec{DeviceSelector: &v1alpha1.DeviceSelector{Paths: tt.paths, OptionalPaths: tt.optional}, RAIDConfig: tt.raid}}
			current := &lvm.VolumeGroup{Name: "vg1"}
			for _, pv := range tt.pvs {
				current.PVs = append(current.PVs, lvm.PhysicalVolume{PvName: pv})
			}
			resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) {
				if path == "/dev/mapper/broken" {
					return "", errors.New("missing symlink")
				}
				if resolved, ok := tt.aliases[path]; ok {
					return resolved, nil
				}
				return path, nil
			})
			removed, err := r.deleteRemovedDevices(ctx, current, vg, resolver)
			if tt.wantError == "" {
				require.NoError(t, err)
				require.Equal(t, len(tt.remove) > 0, removed)
			} else {
				require.ErrorContains(t, err, tt.wantError)
				require.False(t, removed)
			}
		})
	}
}

// NormalEvent used to return early unless V(1) logging was enabled, which silently
// dropped the DeviceAdded and DeviceRemoved audit trail in every default install. The
// context here carries no logger, so V(1) is off — the events must still be recorded.
func TestNormalEventIsNotGatedOnLogVerbosity(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	recorder := events.NewFakeRecorder(10)
	r := &Reconciler{
		Client:        fake.NewClientBuilder().WithScheme(scheme).Build(),
		EventRecorder: recorder,
		NodeName:      "node1",
		Namespace:     "ns1",
	}
	vg := &v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "ns1"}}

	r.NormalEvent(context.Background(), vg, EventReasonDeviceAdded, "added /dev/loop0")

	select {
	case event := <-recorder.Events:
		require.Contains(t, event, string(EventReasonDeviceAdded))
		require.Contains(t, event, "added /dev/loop0")
	default:
		t.Fatal("NormalEvent recorded nothing at default log verbosity")
	}
}

func TestGlobDiscoveryRequeues(t *testing.T) {
	r := &Reconciler{}
	for _, policy := range []v1alpha1.DeviceDiscoveryPolicySpec{v1alpha1.DeviceDiscoveryPolicyStatic, v1alpha1.DeviceDiscoveryPolicyDynamic} {
		for _, optional := range []bool{false, true} {
			selector := &v1alpha1.DeviceSelector{}
			if optional {
				selector.OptionalPaths = []v1alpha1.DevicePath{"glob:/dev/sd[b-d]"}
			} else {
				selector.Paths = []v1alpha1.DevicePath{"glob:/dev/sd[b-d]"}
			}
			vg := &v1alpha1.LVMVolumeGroup{Spec: v1alpha1.LVMVolumeGroupSpec{DeviceSelector: selector}}
			require.Equal(t, reconcileAgain, r.determineFinishedRequeue(vg, policy))
			vg.Spec.RAIDConfig = &v1alpha1.RAIDConfig{Type: v1alpha1.RAIDTypeRAID1}
			require.Equal(t, raidReconcileInterval, r.determineFinishedRequeue(vg, policy).RequeueAfter)
		}
	}
}

func TestGlobSafetyFilters(t *testing.T) {
	for _, optional := range []bool{false, true} {
		selector := &v1alpha1.DeviceSelector{}
		if optional {
			selector.OptionalPaths = []v1alpha1.DevicePath{"glob:/dev/sd[b-z]*"}
		} else {
			selector.Paths = []v1alpha1.DevicePath{"glob:/dev/sd[b-z]*"}
		}
		vg := &v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{Name: "vg1"}, Spec: v1alpha1.LVMVolumeGroupSpec{DeviceSelector: selector}}
		devices := []lsblk.BlockDevice{
			{Name: "/dev/sdb", KName: "/dev/sdb", Type: "disk"},
			{Name: "/dev/sdc", KName: "/dev/sdc", Type: "disk", ReadOnly: true},
			{Name: "/dev/sdd", KName: "/dev/sdd", Type: "disk", FSType: "ext4"},
			{Name: "/dev/sde", KName: "/dev/sde", Type: "disk", FSType: filter.FSTypeLVM2Member},
			{Name: "/dev/sdf", KName: "/dev/sdf", Type: "disk", Children: []lsblk.BlockDevice{{Name: "/dev/sdf1", KName: "/dev/sdf1", Type: "part"}}},
		}
		resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) { return path, nil })
		filtered := filterDevices(context.Background(), devices, resolver, filter.DefaultFilters(context.Background(), &filter.Options{VG: vg, PVs: []lvm.PhysicalVolume{{PvName: "/dev/sde", VgName: "other-vg"}}}))
		var available []string
		for _, device := range filtered.Available {
			available = append(available, device.KName)
		}
		require.ElementsMatch(t, []string{"/dev/sdb", "/dev/sdf1"}, available)
		if !optional {
			require.Error(t, VerifyMandatoryDevicePaths(filtered, resolver, selector.Paths))
		}
	}
}

// Hand-created LVMVolumeGroups bypass the LVMCluster webhook, so the node repeats the
// RAID check rather than trusting admission.
func TestGlobRejectedWithRAIDOnNode(t *testing.T) {
	selector := &v1alpha1.DeviceSelector{Paths: []v1alpha1.DevicePath{"glob:/dev/sd[a-d]"}}
	spec := &v1alpha1.LVMVolumeGroupSpec{DeviceSelector: selector}
	require.NoError(t, validateDeviceSelector(spec))

	spec.RAIDConfig = &v1alpha1.RAIDConfig{Type: v1alpha1.RAIDTypeRAID5}
	require.ErrorIs(t, validateDeviceSelector(spec), v1alpha1.ErrRAIDGlobPathsUnsupported)

	spec.DeviceSelector = &v1alpha1.DeviceSelector{Paths: []v1alpha1.DevicePath{"/dev/sda", "/dev/sdb", "/dev/sdc"}}
	require.NoError(t, validateDeviceSelector(spec))
}

// A pattern keeps discovering devices, so reporting the set as Preconfigured would be
// wrong even though explicit paths are present.
func TestGlobReportsRuntimeDynamicDiscovery(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))

	for _, tt := range []struct {
		name  string
		paths []v1alpha1.DevicePath
		want  v1alpha1.DeviceDiscoveryPolicyStatus
	}{
		{"glob paths", []v1alpha1.DevicePath{"glob:/dev/sd*"}, v1alpha1.DeviceDiscoveryPolicyRuntimeDynamic},
		{"literal paths", []v1alpha1.DevicePath{"/dev/sdb"}, v1alpha1.DeviceDiscoveryPolicyPreconfigured},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := &Reconciler{
				NodeName:  "node1",
				Namespace: "ns",
				Scheme:    scheme,
				Client:    fake.NewClientBuilder().WithScheme(scheme).Build(),
			}
			vg := &v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "ns"},
				Spec:       v1alpha1.LVMVolumeGroupSpec{DeviceSelector: &v1alpha1.DeviceSelector{Paths: tt.paths}},
			}
			status := &v1alpha1.VGStatus{Name: "vg1"}
			_, err := r.setVolumeGroupStatus(context.Background(), vg, status)
			require.NoError(t, err)
			require.Equal(t, tt.want, status.DeviceDiscoveryPolicy)
		})
	}
}
