package vgmanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	wipefsmocks "github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/wipefs/mocks"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

// Files model the host's device directory; resolution maps them to mock kernel
// paths. No block devices or LVM commands are needed for these tests.
func globDeviceFixture(t *testing.T) (v1alpha1.DevicePath, *symlinkResolver.Resolver) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"sda", "sdb", "sdc", "nvme0n1"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0600))
	}
	return v1alpha1.DevicePath("glob:" + filepath.Join(dir, "sd*")),
		symlinkResolver.NewWithResolver(func(path string) (string, error) {
			return "/dev/" + filepath.Base(path), nil
		})
}

func TestGlobDeviceSelection(t *testing.T) {
	for _, optional := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "optional"}[optional], func(t *testing.T) {
			path, resolver := globDeviceFixture(t)
			selector := &v1alpha1.DeviceSelector{Paths: []v1alpha1.DevicePath{path}}
			if optional {
				selector.OptionalPaths, selector.Paths = selector.Paths, nil
			}
			vg := &v1alpha1.LVMVolumeGroup{}
			vg.Name = "vg1"
			vg.Spec.DeviceSelector = selector
			devices := []lsblk.BlockDevice{
				{Name: "/dev/sda", KName: "/dev/sda", Type: "disk"},
				{Name: "/dev/sdb", KName: "/dev/sdb", Type: "disk", ReadOnly: true},
				{Name: "/dev/sdc", KName: "/dev/sdc", Type: "disk", FSType: filter.FSTypeLVM2Member},
				{Name: "/dev/nvme0n1", KName: "/dev/nvme0n1", Type: "disk"},
			}
			mounts := []*string{nil}
			for i := range devices {
				devices[i].Mountpoints = &mounts
			}
			filters := filter.DefaultFilters(context.Background(), &filter.Options{
				VG: vg, PVs: []lvm.PhysicalVolume{{PvName: "/dev/sdc", VgName: "vg1"}},
			})
			filtered := filterDevices(context.Background(), devices, resolver, filters)
			require.Equal(t, []lsblk.BlockDevice{devices[0]}, filtered.Available)
			require.ErrorContains(t, filtered.FilterErrors("/dev/sdb")[0], "read-only")
			require.ErrorIs(t, filtered.FilterErrors("/dev/sdc")[0], filter.ErrDeviceAlreadySetupCorrectly)
			require.NotEmpty(t, filtered.FilterErrors("/dev/nvme0n1"))
			err := VerifyMandatoryDevicePaths(filtered, resolver, selector.Paths)
			require.NoError(t, err)

			devices[1].ReadOnly = false
			filtered = filterDevices(context.Background(), devices, resolver, filters)
			require.Len(t, filtered.Available, 2)
			// A matching device already in the VG satisfies a required pattern.
			require.NoError(t, VerifyMandatoryDevicePaths(filtered, resolver, selector.Paths))
		})
	}
}

func TestRequiredGlobNeedsAUsableOrExistingDevice(t *testing.T) {
	path, resolver := globDeviceFixture(t)
	vg := &v1alpha1.LVMVolumeGroup{}
	vg.Name = "vg1"
	vg.Spec.DeviceSelector = &v1alpha1.DeviceSelector{Paths: []v1alpha1.DevicePath{path}}
	devices := []lsblk.BlockDevice{
		{Name: "/dev/sda", KName: "/dev/sda", Type: "disk", ReadOnly: true},
		{Name: "/dev/sdb", KName: "/dev/sdb", Type: "disk", FSType: "ext4"},
		{Name: "/dev/sdc", KName: "/dev/sdc", Type: "disk", FSType: filter.FSTypeLVM2Member},
	}
	mounts := []*string{nil}
	for i := range devices {
		devices[i].Mountpoints = &mounts
	}
	filters := filter.DefaultFilters(context.Background(), &filter.Options{VG: vg})
	filtered := filterDevices(context.Background(), devices, resolver, filters)
	require.Empty(t, filtered.Available)
	err := VerifyMandatoryDevicePaths(filtered, resolver, vg.Spec.DeviceSelector.Paths)
	require.ErrorContains(t, err, "no usable or already configured devices")
	require.ErrorContains(t, err, "read-only")
	require.ErrorContains(t, err, "invalid filesystem signature")
	require.ErrorContains(t, err, "not visible")

	// An optional glob may have no eligible devices; overall VG creation still
	// needs another available device or an existing volume group.
	require.NoError(t, VerifyMandatoryDevicePaths(filtered, resolver, nil))

	// A device already in this VG satisfies the required glob even when the
	// other matches remain excluded.
	filters = filter.DefaultFilters(context.Background(), &filter.Options{
		VG: vg, PVs: []lvm.PhysicalVolume{{PvName: "/dev/sdc", VgName: "vg1"}},
	})
	filtered = filterDevices(context.Background(), devices, resolver, filters)
	require.Empty(t, filtered.Available)
	require.NoError(t, VerifyMandatoryDevicePaths(filtered, resolver, vg.Spec.DeviceSelector.Paths))
}

func TestRequiredGlobWithLogicalVolumeChildren(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"sda", "dm-0"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0600))
	}
	path := v1alpha1.DevicePath("glob:" + filepath.Join(dir, "*"))
	vg := &v1alpha1.LVMVolumeGroup{}
	vg.Name = "vg1"
	vg.Spec.DeviceSelector = &v1alpha1.DeviceSelector{Paths: []v1alpha1.DevicePath{path}}
	physical := testWipeDisk("/dev/sda", "8:0")
	physical.FSType = filter.FSTypeLVM2Member
	logical := testWipeDisk("/dev/dm-0", "253:0")
	logical.Type = lsblk.DeviceTypeLVM
	physical.Children = []lsblk.BlockDevice{logical}
	for _, owner := range []string{"vg1", "vg2"} {
		t.Run(owner, func(t *testing.T) {
			resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) {
				return "/dev/" + filepath.Base(path), nil
			})
			filtered := filterDevices(context.Background(), []lsblk.BlockDevice{physical}, resolver, filter.DefaultFilters(context.Background(), &filter.Options{
				VG: vg, PVs: []lvm.PhysicalVolume{{PvName: "/dev/sda", VgName: owner}},
			}))
			require.Empty(t, filtered.Available)
			err := VerifyMandatoryDevicePaths(filtered, resolver, vg.Spec.DeviceSelector.Paths)
			if owner == vg.Name {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "no usable or already configured devices")
			}
		})
	}
}

func TestGlobRejectsUnassignedPVWithoutFilesystemSignature(t *testing.T) {
	for _, optional := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "optional"}[optional], func(t *testing.T) {
			path, resolver := globDeviceFixture(t)
			vg := &v1alpha1.LVMVolumeGroup{}
			vg.Name = "vg1"
			vg.Spec.DeviceSelector = &v1alpha1.DeviceSelector{Paths: []v1alpha1.DevicePath{path}}
			if optional {
				vg.Spec.DeviceSelector.OptionalPaths, vg.Spec.DeviceSelector.Paths = vg.Spec.DeviceSelector.Paths, nil
			}
			devices := []lsblk.BlockDevice{testWipeDisk("/dev/sda", "8:0")}
			filtered := filterDevices(context.Background(), devices, resolver, filter.DefaultFilters(context.Background(), &filter.Options{
				VG: vg, PVs: []lvm.PhysicalVolume{{PvName: "/dev/sda"}},
			}))
			require.Empty(t, filtered.Available)
			require.ErrorContains(t, errors.Join(filtered.FilterErrors("/dev/sda")...), "unassigned physical volume")
		})
	}
}

func TestRequiredGlobCanUseResolvedMatchesWhenAnAliasIsBroken(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	device := filepath.Join(dir, "disk")
	require.NoError(t, os.WriteFile(device, nil, 0600))
	require.NoError(t, os.Symlink(device, filepath.Join(dir, "alias-good")))
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "alias-broken")))
	path := v1alpha1.DevicePath("glob:" + filepath.Join(dir, "alias-*"))
	filtered := FilteredBlockDevices{Available: []lsblk.BlockDevice{{KName: device}}}
	resolver := symlinkResolver.NewWithDefaultResolver()
	require.NoError(t, VerifyMandatoryDevicePaths(filtered, resolver, []v1alpha1.DevicePath{path}))
	for _, optional := range []bool{false, true} {
		selector := &v1alpha1.DeviceSelector{Paths: []v1alpha1.DevicePath{path}}
		if optional {
			selector.OptionalPaths, selector.Paths = selector.Paths, nil
		}
		vg := &v1alpha1.LVMVolumeGroup{Spec: v1alpha1.LVMVolumeGroupSpec{DeviceSelector: selector}}
		paths, err := buildDevicePathMappings(context.Background(), vg, resolver, lvm.PhysicalVolume{PvName: device})
		require.NoError(t, err)
		require.Contains(t, paths, device)
	}
}

func TestGlobDevicePathMappings(t *testing.T) {
	path, resolver := globDeviceFixture(t)
	vg := &v1alpha1.LVMVolumeGroup{}
	vg.Name = "vg1"
	vg.Spec.DeviceSelector = &v1alpha1.DeviceSelector{Paths: []v1alpha1.DevicePath{path}}
	paths, err := buildDevicePathMappings(context.Background(), vg, resolver)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"/dev/sda", "/dev/sdb", "/dev/sdc"}, paths)

	// Devices selected through a glob must not be mistaken for removal requests.
	r := &Reconciler{}
	removed, err := r.deleteRemovedDevices(context.Background(), &lvm.VolumeGroup{
		Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/sda"}, {PvName: "/dev/sdb"}},
	}, vg, resolver)
	require.NoError(t, err)
	require.False(t, removed)
}

func TestMissingGlobDevicePaths(t *testing.T) {
	for _, pattern := range []string{"missing*", "invalid["} {
		t.Run(pattern, func(t *testing.T) {
			path := v1alpha1.DevicePath("glob:" + filepath.Join(t.TempDir(), pattern))
			resolver := symlinkResolver.NewWithDefaultResolver()
			require.Error(t, VerifyMandatoryDevicePaths(FilteredBlockDevices{}, resolver, []v1alpha1.DevicePath{path}))
			vg := &v1alpha1.LVMVolumeGroup{}
			vg.Spec.DeviceSelector = &v1alpha1.DeviceSelector{OptionalPaths: []v1alpha1.DevicePath{path}}
			paths, err := buildDevicePathMappings(context.Background(), vg, resolver)
			if pattern == "invalid[" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Empty(t, paths)
		})
	}
}

func TestReduceGlobMatchesAcrossDeviceTree(t *testing.T) {
	devices := []lsblk.BlockDevice{{KName: "/dev/sda", Children: []lsblk.BlockDevice{{KName: "/dev/sda1"}, {KName: "/dev/sda2"}}}, {KName: "/dev/sdb"}}
	matches := []string{"/dev/sda", "/dev/sda1", "/dev/sda2", "/dev/sdb", "/dev/not-a-block-device"}
	require.Equal(t, []string{"/dev/sda1", "/dev/sda2", "/dev/sdb"}, reduceGlobMatches(matches, devices, false))
	require.Equal(t, []string{"/dev/sda", "/dev/sdb"}, reduceGlobMatches(matches, devices, true))
}

func TestRemovalStopsOnUnresolvedOptionalAliasGlob(t *testing.T) {
	vg := &v1alpha1.LVMVolumeGroup{}
	vg.Name = "vg1"
	vg.Spec.DeviceSelector = &v1alpha1.DeviceSelector{
		Paths:         []v1alpha1.DevicePath{"/dev/sda"},
		OptionalPaths: []v1alpha1.DevicePath{v1alpha1.DevicePath("glob:" + filepath.Join(t.TempDir(), "disk-*"))},
	}
	resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) { return path, nil })
	r := &Reconciler{}
	removed, err := r.deleteRemovedDevices(context.Background(), &lvm.VolumeGroup{
		Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/sda"}, {PvName: "/dev/sdb"}},
	}, vg, resolver)
	require.ErrorContains(t, err, "no alias under")
	require.False(t, removed)
}

func TestRemovalStopsWhenOnlySomeAliasesMatch(t *testing.T) {
	dir := t.TempDir()
	alias := filepath.Join(dir, "disk-one")
	require.NoError(t, os.WriteFile(alias, nil, 0600))
	vg := &v1alpha1.LVMVolumeGroup{}
	vg.Name = "vg1"
	vg.Spec.DeviceSelector = &v1alpha1.DeviceSelector{
		OptionalPaths: []v1alpha1.DevicePath{v1alpha1.DevicePath("glob:" + filepath.Join(dir, "disk-*"))},
	}
	resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) {
		if path == alias {
			return "/dev/sda", nil
		}
		return path, nil
	})
	r := &Reconciler{}
	removed, err := r.deleteRemovedDevices(context.Background(), &lvm.VolumeGroup{
		Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/sda"}, {PvName: "/dev/sdb"}},
	}, vg, resolver)
	require.ErrorContains(t, err, "no alias under")
	require.False(t, removed)
}

// Narrowing an alias pattern has to reduce the volume group. The devices an admin
// drops are exactly the ones that stop matching, so treating every unmatched PV as a
// possible udev race left no way to shrink a volume group addressed by alias: it
// failed every reconcile instead, reporting a missing alias that was present.
func TestNarrowingAnAliasGlobRemovesDeselectedPVs(t *testing.T) {
	dir := t.TempDir()
	aliases := map[string]string{}
	for _, name := range []string{"disk0", "disk1", "disk2", "disk3"} {
		alias := filepath.Join(dir, "lvmstest-"+name)
		require.NoError(t, os.WriteFile(alias, nil, 0600))
		aliases[alias] = "/dev/" + name
	}
	resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) {
		if device, ok := aliases[path]; ok {
			return device, nil
		}
		return path, nil
	})
	vg := &v1alpha1.LVMVolumeGroup{}
	vg.Name = "vg1"
	vg.Spec.DeviceSelector = &v1alpha1.DeviceSelector{
		Paths: []v1alpha1.DevicePath{v1alpha1.DevicePath("glob:" + filepath.Join(dir, "lvmstest-disk[03]"))},
	}
	paths, err := buildDevicePathMappings(context.Background(), vg, resolver,
		lvm.PhysicalVolume{PvName: "/dev/disk0"}, lvm.PhysicalVolume{PvName: "/dev/disk1"},
		lvm.PhysicalVolume{PvName: "/dev/disk2"}, lvm.PhysicalVolume{PvName: "/dev/disk3"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"/dev/disk0", "/dev/disk3"}, paths)

	// The same narrowing stays refused while an alias is genuinely absent, because
	// then the directory cannot account for the PV at all.
	require.NoError(t, os.Remove(filepath.Join(dir, "lvmstest-disk1")))
	_, err = buildDevicePathMappings(context.Background(), vg, resolver.Fresh(),
		lvm.PhysicalVolume{PvName: "/dev/disk0"}, lvm.PhysicalVolume{PvName: "/dev/disk1"})
	require.ErrorContains(t, err, "no alias under")
}

func TestRequiredGlobKeepsPVWithMissingDeviceNode(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "sda"), filepath.Join(dir, "sdb")
	require.NoError(t, os.WriteFile(first, nil, 0600))
	vg := &v1alpha1.LVMVolumeGroup{Spec: v1alpha1.LVMVolumeGroupSpec{DeviceSelector: &v1alpha1.DeviceSelector{
		Paths: []v1alpha1.DevicePath{v1alpha1.DevicePath("glob:" + filepath.Join(dir, "sd*"))},
	}}}
	resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) { return path, nil })
	paths, err := buildDevicePathMappings(context.Background(), vg, resolver,
		lvm.PhysicalVolume{PvName: first}, lvm.PhysicalVolume{PvName: second})
	require.NoError(t, err)
	require.Contains(t, paths, first)
	require.Contains(t, paths, second)
}

func TestRemovalStopsWhenPVIdentityCannotBeResolved(t *testing.T) {
	vg := &v1alpha1.LVMVolumeGroup{Spec: v1alpha1.LVMVolumeGroupSpec{DeviceSelector: &v1alpha1.DeviceSelector{
		Paths: []v1alpha1.DevicePath{"/dev/sda"},
	}}}
	resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) {
		if path == "/dev/unknown" {
			return "", errors.New("device path unavailable")
		}
		return path, nil
	})
	r := &Reconciler{}
	removed, err := r.deleteRemovedDevices(context.Background(), &lvm.VolumeGroup{
		Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/sda"}, {PvName: "/dev/unknown"}},
	}, vg, resolver)
	require.ErrorContains(t, err, "cannot determine whether PV /dev/unknown is still selected")
	require.False(t, removed)
}

func TestGlobDiscoveryRequeue(t *testing.T) {
	r := &Reconciler{}
	vg := &v1alpha1.LVMVolumeGroup{}
	for _, optional := range []bool{false, true} {
		vg.Spec.DeviceSelector = &v1alpha1.DeviceSelector{Paths: []v1alpha1.DevicePath{"glob:/dev/sd*"}}
		if optional {
			vg.Spec.DeviceSelector.OptionalPaths, vg.Spec.DeviceSelector.Paths = vg.Spec.DeviceSelector.Paths, nil
		}
		for _, policy := range []v1alpha1.DeviceDiscoveryPolicySpec{
			v1alpha1.DeviceDiscoveryPolicyStatic, v1alpha1.DeviceDiscoveryPolicyDynamic,
		} {
			require.Equal(t, reconcileAgain, r.determineFinishedRequeue(vg, policy))
		}
	}
	vg.Spec.DeviceSelector = &v1alpha1.DeviceSelector{Paths: []v1alpha1.DevicePath{"/dev/sda"}}
	require.Zero(t, r.determineFinishedRequeue(vg, v1alpha1.DeviceDiscoveryPolicyDynamic))
}

func TestGlobWipeOwnershipGuard(t *testing.T) {
	for _, tc := range []struct {
		name            string
		owner           string
		optional        bool
		partition       bool
		alias           bool
		resolutionError bool
		wantError       bool
		wipeFree        bool
	}{
		{name: "foreign device class aborts the entire required wipe", owner: "vg2", wantError: true},
		{name: "unmanaged VG is also protected", owner: "system", wantError: true},
		{name: "optional foreign device is skipped", owner: "vg2", optional: true, wipeFree: true},
		{name: "same VG is never wiped", owner: "vg1", wipeFree: true},
		{name: "foreign PV on a partition protects its parent", owner: "vg2", partition: true, wantError: true},
		{name: "symlink alias cannot bypass ownership", owner: "vg2", alias: true, wantError: true},
		{name: "unknown ownership fails closed", owner: "vg2", resolutionError: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) {
				if path == "/dev/disk/by-id/owned" {
					if tc.resolutionError {
						return "", errors.New("ownership path is unavailable")
					}
					return "/dev/sdb", nil
				}
				return "/dev/" + filepath.Base(path), nil
			})
			vg := &v1alpha1.LVMVolumeGroup{}
			vg.Name = "vg1"
			vg.Spec.DeviceSelector = &v1alpha1.DeviceSelector{
				Paths: []v1alpha1.DevicePath{"/dev/sda", "/dev/sdb"}, ForceWipeDevicesAndDestroyAllData: ptr.To(true),
			}
			if tc.optional {
				vg.Spec.DeviceSelector.OptionalPaths, vg.Spec.DeviceSelector.Paths = vg.Spec.DeviceSelector.Paths, nil
			}
			devices := []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/sdb"}}
			pv := lvm.PhysicalVolume{PvName: "/dev/sdb", VgName: tc.owner}
			if tc.partition {
				pv.PvName = "/dev/sdb1"
				devices[1].Children = []lsblk.BlockDevice{{KName: "/dev/sdb1", Type: "part"}}
			}
			if tc.alias || tc.resolutionError {
				pv.PvName = "/dev/disk/by-id/owned"
			}
			wipefs := wipefsmocks.NewMockWipefs(t)
			if tc.wipeFree {
				wipefs.EXPECT().Wipe(context.Background(), "/dev/sda").Return(nil).Once()
			}
			r := &Reconciler{NodeName: "node", Wipefs: wipefs}
			wiped, err := r.wipeDevices(context.Background(), vg, devices, []lvm.PhysicalVolume{pv}, resolver)
			if tc.wantError {
				require.ErrorContains(t, err, "refusing to wipe")
				require.Empty(t, vg.Annotations)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wipeFree, wiped)
			// No expectation permits wiping the protected PV, its parent, or
			// any free device before a required ownership conflict is reported.
			wipefs.AssertNotCalled(t, "Wipe", context.Background(), "/dev/sdb")
		})
	}
}

func TestGlobCannotAdoptForeignPVWithoutFilesystemSignature(t *testing.T) {
	path, resolver := globDeviceFixture(t)
	vg := &v1alpha1.LVMVolumeGroup{}
	vg.Name = "vg1"
	vg.Spec.DeviceSelector = &v1alpha1.DeviceSelector{Paths: []v1alpha1.DevicePath{path}}
	mounts := []*string{nil}
	devices := []lsblk.BlockDevice{{Name: "/dev/sdb", KName: "/dev/sdb", Type: "disk", Mountpoints: &mounts}}
	filtered := filterDevices(context.Background(), devices, resolver, filter.DefaultFilters(context.Background(), &filter.Options{
		VG: vg, PVs: []lvm.PhysicalVolume{{PvName: "/dev/sdb", VgName: "vg2"}},
	}))
	require.Empty(t, filtered.Available)
	require.ErrorContains(t, filtered.FilterErrors("/dev/sdb")[0], "another volume group")
}
