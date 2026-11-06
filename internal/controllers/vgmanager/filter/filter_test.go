package filter

import (
	"context"
	"fmt"
	"testing"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	"github.com/stretchr/testify/assert"
)

type filterTestCase struct {
	label     string
	device    lsblk.BlockDevice
	expectErr bool
}

type advancedFilterTestCase struct {
	label           string
	device          lsblk.BlockDevice
	assertErr       assert.ErrorAssertionFunc
	volumeGroupSpec *lvmv1alpha1.LVMVolumeGroupSpec
	lvmExpect       []lvm.PhysicalVolume
}

func TestForeignPVOwnershipCheckAppliesOnlyToGlobSelectors(t *testing.T) {
	device := lsblk.BlockDevice{Name: "/dev/sdb", KName: "/dev/sdb"}
	for _, tc := range []struct {
		name     string
		selector *lvmv1alpha1.DeviceSelector
		pv       lvm.PhysicalVolume
		wantErr  string
		calls    int
	}{
		{name: "no selector", pv: lvm.PhysicalVolume{PvName: "/dev/missing", VgName: "vg2"}},
		{name: "literal selector", selector: &lvmv1alpha1.DeviceSelector{Paths: []lvmv1alpha1.DevicePath{"/dev/sdb"}}, pv: lvm.PhysicalVolume{PvName: "/dev/missing", VgName: "vg2"}},
		{name: "literal selector with unassigned PV", selector: &lvmv1alpha1.DeviceSelector{Paths: []lvmv1alpha1.DevicePath{"/dev/sdb"}}, pv: lvm.PhysicalVolume{PvName: "/dev/alias"}},
		{name: "required glob with foreign PV", selector: &lvmv1alpha1.DeviceSelector{Paths: []lvmv1alpha1.DevicePath{"glob:/dev/sd*"}}, pv: lvm.PhysicalVolume{PvName: "/dev/alias", VgName: "vg2"}, wantErr: "belongs to another volume group", calls: 1},
		{name: "optional glob with foreign PV", selector: &lvmv1alpha1.DeviceSelector{OptionalPaths: []lvmv1alpha1.DevicePath{"glob:/dev/sd*"}}, pv: lvm.PhysicalVolume{PvName: "/dev/alias", VgName: "vg2"}, wantErr: "belongs to another volume group", calls: 1},
		{name: "glob with unresolved foreign PV", selector: &lvmv1alpha1.DeviceSelector{Paths: []lvmv1alpha1.DevicePath{"glob:/dev/sd*"}}, pv: lvm.PhysicalVolume{PvName: "/dev/missing", VgName: "vg2"}, wantErr: "treated as missing", calls: 1},
		{name: "glob with missing foreign PV", selector: &lvmv1alpha1.DeviceSelector{Paths: []lvmv1alpha1.DevicePath{"glob:/dev/sd*"}}, pv: lvm.PhysicalVolume{PvName: "[unknown]", VgName: "vg2", PvMissing: "missing"}},
		{name: "glob with missing named foreign PV", selector: &lvmv1alpha1.DeviceSelector{Paths: []lvmv1alpha1.DevicePath{"glob:/dev/sd*"}}, pv: lvm.PhysicalVolume{PvName: "/dev/missing", VgName: "vg2", PvMissing: "missing"}},
		{name: "glob with unassigned PV", selector: &lvmv1alpha1.DeviceSelector{Paths: []lvmv1alpha1.DevicePath{"glob:/dev/sd*"}}, pv: lvm.PhysicalVolume{PvName: "/dev/alias"}, wantErr: "unassigned physical volume", calls: 1},
		{name: "optional glob with unassigned PV", selector: &lvmv1alpha1.DeviceSelector{OptionalPaths: []lvmv1alpha1.DevicePath{"glob:/dev/sd*"}}, pv: lvm.PhysicalVolume{PvName: "/dev/alias"}, wantErr: "unassigned physical volume", calls: 1},
		{name: "glob with same VG", selector: &lvmv1alpha1.DeviceSelector{Paths: []lvmv1alpha1.DevicePath{"glob:/dev/sd*"}}, pv: lvm.PhysicalVolume{PvName: "/dev/alias", VgName: "vg1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) {
				calls++
				if path == "/dev/missing" {
					return "", fmt.Errorf("missing device")
				}
				return "/dev/sdb", nil
			})
			vg := &lvmv1alpha1.LVMVolumeGroup{}
			vg.Name = "vg1"
			vg.Spec.DeviceSelector = tc.selector
			err := DefaultFilters(context.Background(), &Options{VG: vg, PVs: []lvm.PhysicalVolume{tc.pv}})[notInAnotherVolumeGroup](device, resolver)
			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
			assert.Equal(t, tc.calls, calls)
		})
	}
}

func TestGlobRejectsUnknownAndUnassignedPhysicalVolumes(t *testing.T) {
	vg := &lvmv1alpha1.LVMVolumeGroup{}
	vg.Name = "vg1"
	vg.Spec.DeviceSelector = &lvmv1alpha1.DeviceSelector{Paths: []lvmv1alpha1.DevicePath{"glob:/dev/sd*"}}
	device := lsblk.BlockDevice{Name: "/dev/sdb", KName: "/dev/sdb", FSType: FSTypeLVM2Member}
	resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) { return path, nil })
	for _, tc := range []struct {
		name string
		pvs  []lvm.PhysicalVolume
		want string
	}{
		{name: "hidden PV", want: "not visible"},
		// notInAnotherVolumeGroup owns the unassigned case: it sees such a PV whether or
		// not lsblk reports the signature, so repeating the rule here only printed the
		// same sentence twice in the node status.
		{name: "unassigned PV", pvs: []lvm.PhysicalVolume{{PvName: "/dev/sdb"}}},
		{name: "other VG", pvs: []lvm.PhysicalVolume{{PvName: "/dev/sdb", VgName: "vg2"}}, want: "another volume group"},
		{name: "same VG", pvs: []lvm.PhysicalVolume{{PvName: "/dev/sdb", VgName: "vg1"}}, want: ErrDeviceAlreadySetupCorrectly.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filters := DefaultFilters(context.Background(), &Options{VG: vg, PVs: tc.pvs})
			err := filters[onlyValidFilesystemSignatures](device, resolver)
			if tc.want == "" {
				assert.NoError(t, err)
				assert.ErrorContains(t, filters[notInAnotherVolumeGroup](device, resolver), "unassigned physical volume")
				return
			}
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestGlobRejectsMountedOrUnknownMountState(t *testing.T) {
	vg := &lvmv1alpha1.LVMVolumeGroup{Spec: lvmv1alpha1.LVMVolumeGroupSpec{DeviceSelector: &lvmv1alpha1.DeviceSelector{Paths: []lvmv1alpha1.DevicePath{"glob:/dev/sd*"}}}}
	filter := DefaultFilters(context.Background(), &Options{VG: vg})[globNotMounted]
	device := lsblk.BlockDevice{Name: "/dev/sda"}
	assert.ErrorContains(t, filter(device, nil), "unknown")
	mounted := "/data"
	mounts := []*string{&mounted}
	device.Mountpoints = &mounts
	assert.ErrorContains(t, filter(device, nil), "mounted")
	mounts = []*string{nil}
	assert.NoError(t, filter(device, nil))
}

func TestNotReadOnly(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc false", device: lsblk.BlockDevice{ReadOnly: false}, expectErr: false},
		{label: "tc true", device: lsblk.BlockDevice{ReadOnly: true}, expectErr: true},
	}
	for _, tc := range testcases {
		t.Run(tc.label, func(t *testing.T) {
			err := DefaultFilters(context.Background(), nil)[notReadOnly](tc.device, symlinkResolver.NewWithDefaultResolver())
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNotSuspended(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc suspended", device: lsblk.BlockDevice{State: "suspended"}, expectErr: true},
		{label: "tc live", device: lsblk.BlockDevice{State: "live"}, expectErr: false},
		{label: "tc running", device: lsblk.BlockDevice{State: "running"}, expectErr: false},
	}
	for _, tc := range testcases {
		t.Run(tc.label, func(t *testing.T) {
			err := DefaultFilters(context.Background(), nil)[notSuspended](tc.device, symlinkResolver.NewWithDefaultResolver())
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNoFilesystemSignature(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc no fs", device: lsblk.BlockDevice{FSType: ""}, expectErr: false},
		{label: "tc xfs", device: lsblk.BlockDevice{FSType: "xfs"}, expectErr: true},
		{label: "tc swap", device: lsblk.BlockDevice{FSType: "swap"}, expectErr: true},
	}
	for _, tc := range testcases {
		t.Run(tc.label, func(t *testing.T) {
			err := DefaultFilters(context.Background(), nil)[onlyValidFilesystemSignatures](tc.device, symlinkResolver.NewWithDefaultResolver())
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNoChildren(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc child", device: lsblk.BlockDevice{Name: "dev1", Children: []lsblk.BlockDevice{{Name: "child1"}}}, expectErr: true},
		{label: "tc no child", device: lsblk.BlockDevice{Name: "dev2", Children: []lsblk.BlockDevice{}}, expectErr: false},
	}
	for _, tc := range testcases {
		t.Run(tc.label, func(t *testing.T) {
			err := DefaultFilters(context.Background(), nil)[noChildren](tc.device, symlinkResolver.NewWithDefaultResolver())
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsUsableDeviceType(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc ROM", device: lsblk.BlockDevice{Name: "dev1", Type: "rom"}, expectErr: true},
		{label: "tc Disk", device: lsblk.BlockDevice{Name: "dev2", Type: "disk"}, expectErr: false},
	}
	for _, tc := range testcases {
		err := DefaultFilters(context.Background(), nil)[usableDeviceType](tc.device, symlinkResolver.NewWithDefaultResolver())
		if tc.expectErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestNoBiosBootInPartLabel(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc 1", device: lsblk.BlockDevice{Name: "dev1", PartLabel: ""}, expectErr: false},
		{label: "tc 2", device: lsblk.BlockDevice{Name: "dev2", PartLabel: "abc"}, expectErr: false},
		{label: "tc 3", device: lsblk.BlockDevice{Name: "dev3", PartLabel: "bios"}, expectErr: true},
		{label: "tc 4", device: lsblk.BlockDevice{Name: "dev4", PartLabel: "BIOS"}, expectErr: true},
		{label: "tc 5", device: lsblk.BlockDevice{Name: "dev5", PartLabel: "boot"}, expectErr: true},
		{label: "tc 6", device: lsblk.BlockDevice{Name: "dev6", PartLabel: "BOOT"}, expectErr: true},
		{label: "tc 7", device: lsblk.BlockDevice{Name: "dev2", PartLabel: "abc"}, expectErr: false},
		{label: "tc 8", device: lsblk.BlockDevice{Name: "dev3", PartLabel: "reserved"}, expectErr: true},
		{label: "tc 9", device: lsblk.BlockDevice{Name: "dev4", PartLabel: "RESERVED"}, expectErr: true},
		{label: "tc 10", device: lsblk.BlockDevice{Name: "dev5", PartLabel: "Reserved"}, expectErr: true},
	}
	for _, tc := range testcases {
		t.Run(tc.label, func(t *testing.T) {
			err := DefaultFilters(context.Background(), nil)[noInvalidPartitionLabel](tc.device, symlinkResolver.NewWithDefaultResolver())
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPartOfDeviceSelector(t *testing.T) {
	testcases := []advancedFilterTestCase{
		{label: "match path selector", device: lsblk.BlockDevice{KName: "dev1"},
			volumeGroupSpec: &lvmv1alpha1.LVMVolumeGroupSpec{DeviceSelector: &lvmv1alpha1.DeviceSelector{
				Paths: []lvmv1alpha1.DevicePath{"dev1"},
			}},
			assertErr: assert.NoError,
		},
		{label: "match optional path selector", device: lsblk.BlockDevice{KName: "dev1"},
			volumeGroupSpec: &lvmv1alpha1.LVMVolumeGroupSpec{DeviceSelector: &lvmv1alpha1.DeviceSelector{
				OptionalPaths: []lvmv1alpha1.DevicePath{"dev1"},
			}},
			assertErr: assert.NoError,
		},
		{label: "no match", device: lsblk.BlockDevice{KName: "dev1"},
			volumeGroupSpec: &lvmv1alpha1.LVMVolumeGroupSpec{DeviceSelector: &lvmv1alpha1.DeviceSelector{
				Paths: []lvmv1alpha1.DevicePath{"dev2"},
			}},
			assertErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "is not part of the device selector")
			},
		},
		{label: "no selector", device: lsblk.BlockDevice{KName: "dev1"},
			volumeGroupSpec: &lvmv1alpha1.LVMVolumeGroupSpec{},
			assertErr:       assert.NoError,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.label, func(t *testing.T) {
			vg := &lvmv1alpha1.LVMVolumeGroup{}
			vg.SetName("vg1")
			vg.Spec = *tc.volumeGroupSpec
			err := DefaultFilters(context.Background(), &Options{VG: vg})[partOfDeviceSelector](tc.device, symlinkResolver.NewWithResolver(func(path string) (string, error) { return path, nil }))
			tc.assertErr(t, err, fmt.Sprintf("partOfDeviceSelector(%v)", tc.device))
		})
	}

}

func TestOnlyValidFilesystemSignatures(t *testing.T) {
	testcases := []advancedFilterTestCase{
		{label: "No FSType", device: lsblk.BlockDevice{KName: "dev1", FSType: ""}, assertErr: assert.NoError},
		{
			label:  "Unrecognized FSType",
			device: lsblk.BlockDevice{KName: "dev2", FSType: "random"},
			assertErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "has an invalid filesystem signature")
			},
		},
		{
			label:     "LVM2_Member without pvs",
			device:    lsblk.BlockDevice{KName: "dev1", FSType: FSTypeLVM2Member},
			assertErr: assert.NoError,
			lvmExpect: []lvm.PhysicalVolume{},
		},
		{
			label:     "LVM2_Member with non-matching pvs",
			device:    lsblk.BlockDevice{KName: "dev1", FSType: FSTypeLVM2Member},
			assertErr: assert.NoError,
			lvmExpect: []lvm.PhysicalVolume{{PvName: "random"}},
		},
		{
			label:  "LVM2_Member with matching pvs,no children,mismatching vg",
			device: lsblk.BlockDevice{KName: "dev1", FSType: FSTypeLVM2Member},
			assertErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "already a LVM2_Member of another volume group")
			},
			lvmExpect: []lvm.PhysicalVolume{{PvName: "dev1", VgName: "random"}},
		},
		{
			label:  "LVM2_Member with matching pvs,no children,matching vg without free space",
			device: lsblk.BlockDevice{KName: "dev1", FSType: FSTypeLVM2Member},
			assertErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "reported as having no free capacity as a physical volume")
			},
			lvmExpect: []lvm.PhysicalVolume{{PvName: "dev1", PvFree: "0G"}},
		},
		{
			label:  "LVM2_Member with matching pvs,children,mismatching vg",
			device: lsblk.BlockDevice{KName: "dev1", FSType: FSTypeLVM2Member, Children: []lsblk.BlockDevice{{KName: "child"}}},
			assertErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "is already a LVM2_Member of another volume group (othervg) and cannot be used for the volume group vg1")
			},
			lvmExpect: []lvm.PhysicalVolume{{PvName: "dev1", VgName: "othervg"}},
		},
		{
			label:  "LVM2_Member that was already setup correctly",
			device: lsblk.BlockDevice{KName: "dev1", FSType: FSTypeLVM2Member, Children: []lsblk.BlockDevice{{KName: "child"}}},
			assertErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorIs(t, err, ErrDeviceAlreadySetupCorrectly)
			},
			lvmExpect: []lvm.PhysicalVolume{{PvName: "dev1", VgName: "vg1"}},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.label, func(t *testing.T) {
			vg := &lvmv1alpha1.LVMVolumeGroup{}
			vg.SetName("vg1")

			err := DefaultFilters(context.Background(), &Options{
				VG:  vg,
				PVs: tc.lvmExpect,
			})[onlyValidFilesystemSignatures](tc.device, symlinkResolver.NewWithResolver(func(path string) (string, error) { return path, nil }))
			tc.assertErr(t, err, fmt.Sprintf("onlyValidFilesystemSignatures(%v)", tc.device))
		})
	}
}
