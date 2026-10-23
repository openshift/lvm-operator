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

func TestGlobSelector(t *testing.T) {
	for _, optional := range []bool{false, true} {
		selector := &lvmv1alpha1.DeviceSelector{}
		paths := []lvmv1alpha1.DevicePath{"glob:/dev/sdb", "glob:/dev/nvme*n1"}
		if optional {
			selector.OptionalPaths = paths
		} else {
			selector.Paths = paths
		}
		vg := &lvmv1alpha1.LVMVolumeGroup{Spec: lvmv1alpha1.LVMVolumeGroupSpec{DeviceSelector: selector}}
		filters := DefaultFilters(context.Background(), &Options{VG: vg})
		resolver := symlinkResolver.NewWithResolver(func(string) (string, error) { t.Fatal("glob must not resolve symlinks"); return "", nil })
		for _, tt := range []struct {
			device  string
			matches bool
		}{
			{"/dev/sdb", true},
			{"/dev/nvme12n1", true},
			{"/dev/sdb1", false},
			{"/prefix/dev/nvme0n1", false},
			{"/dev/sda", false},
		} {
			err := filters[partOfDeviceSelector](lsblk.BlockDevice{Name: tt.device, KName: tt.device}, resolver)
			if tt.matches {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		}
	}
}

// A malformed pattern must exclude the device, never fall through to "selected".
func TestGlobSelectorRejectsMalformedPattern(t *testing.T) {
	vg := &lvmv1alpha1.LVMVolumeGroup{Spec: lvmv1alpha1.LVMVolumeGroupSpec{
		DeviceSelector: &lvmv1alpha1.DeviceSelector{Paths: []lvmv1alpha1.DevicePath{"glob:/dev/["}},
	}}
	filters := DefaultFilters(context.Background(), &Options{VG: vg})
	resolver := symlinkResolver.NewWithResolver(func(path string) (string, error) { return path, nil })
	// The whole safety chain stays intact even when the selector cannot be evaluated.
	for _, name := range []string{partOfDeviceSelector, notReadOnly, noChildren} {
		assert.Contains(t, filters, name)
	}
	assert.ErrorContains(t, filters[partOfDeviceSelector](lsblk.BlockDevice{Name: "/dev/sdb", KName: "/dev/sdb"}, resolver), "invalid device path pattern")
}
