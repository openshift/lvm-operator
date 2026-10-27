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
		// Regex matching - basic cases
		{label: "regex path matches device", device: lsblk.BlockDevice{KName: "/dev/nvme0n1"},
			volumeGroupSpec: &lvmv1alpha1.LVMVolumeGroupSpec{DeviceSelector: &lvmv1alpha1.DeviceSelector{
				Paths: []lvmv1alpha1.DevicePath{"regex:/dev/nvme[0-9]n1"},
			}},
			assertErr: assert.NoError,
		},
		{label: "regex optional path matches device", device: lsblk.BlockDevice{KName: "/dev/nvme0n1"},
			volumeGroupSpec: &lvmv1alpha1.LVMVolumeGroupSpec{DeviceSelector: &lvmv1alpha1.DeviceSelector{
				OptionalPaths: []lvmv1alpha1.DevicePath{"regex:/dev/nvme[0-9]n1"},
			}},
			assertErr: assert.NoError,
		},
		{label: "regex rejects partition when only base device matches", device: lsblk.BlockDevice{KName: "/dev/nvme1n1p1"},
			volumeGroupSpec: &lvmv1alpha1.LVMVolumeGroupSpec{DeviceSelector: &lvmv1alpha1.DeviceSelector{
				Paths: []lvmv1alpha1.DevicePath{"regex:/dev/nvme[0-9]n1"},
			}},
			assertErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "is not part of the device selector")
			},
		},
		{label: "regex does not match unrelated device", device: lsblk.BlockDevice{KName: "/dev/sda"},
			volumeGroupSpec: &lvmv1alpha1.LVMVolumeGroupSpec{DeviceSelector: &lvmv1alpha1.DeviceSelector{
				Paths: []lvmv1alpha1.DevicePath{"regex:/dev/nvme[0-9]n1"},
			}},
			assertErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "is not part of the device selector")
			},
		},
		{label: "literal path resolved while regex is not", device: lsblk.BlockDevice{KName: "/dev/sda"},
			volumeGroupSpec: &lvmv1alpha1.LVMVolumeGroupSpec{DeviceSelector: &lvmv1alpha1.DeviceSelector{
				Paths: []lvmv1alpha1.DevicePath{"regex:/dev/nvme[0-9]n1", "/dev/sda"},
			}},
			assertErr: assert.NoError,
		},
		{label: "alternation matches first branch", device: lsblk.BlockDevice{KName: "/dev/sda"},
			volumeGroupSpec: &lvmv1alpha1.LVMVolumeGroupSpec{DeviceSelector: &lvmv1alpha1.DeviceSelector{
				Paths: []lvmv1alpha1.DevicePath{"regex:/dev/sda|/dev/sdb"},
			}},
			assertErr: assert.NoError,
		},
		{label: "anchored alternation rejects prefix mismatch", device: lsblk.BlockDevice{KName: "/dev/sdaX"},
			volumeGroupSpec: &lvmv1alpha1.LVMVolumeGroupSpec{DeviceSelector: &lvmv1alpha1.DeviceSelector{
				Paths: []lvmv1alpha1.DevicePath{"regex:/dev/sda|/dev/sdb"},
			}},
			assertErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "is not part of the device selector")
			},
		},
		{label: "invalid regex pattern does not match any device", device: lsblk.BlockDevice{KName: "/dev/sda"},
			volumeGroupSpec: &lvmv1alpha1.LVMVolumeGroupSpec{DeviceSelector: &lvmv1alpha1.DeviceSelector{
				Paths: []lvmv1alpha1.DevicePath{"regex:/dev/sd["},
			}},
			assertErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "is not part of the device selector")
			},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.label, func(t *testing.T) {
			vg := &lvmv1alpha1.LVMVolumeGroup{}
			vg.SetName("vg1")
			vg.Spec = *tc.volumeGroupSpec

			resolverCalls := []string{}
			tracingResolver := symlinkResolver.NewWithResolver(func(path string) (string, error) {
				resolverCalls = append(resolverCalls, string(path))
				return path, nil
			})

			var originalPaths []lvmv1alpha1.DevicePath
			var originalOptPaths []lvmv1alpha1.DevicePath
			if vg.Spec.DeviceSelector != nil {
				originalPaths = make([]lvmv1alpha1.DevicePath, len(vg.Spec.DeviceSelector.Paths))
				originalOptPaths = make([]lvmv1alpha1.DevicePath, len(vg.Spec.DeviceSelector.OptionalPaths))
				copy(originalPaths, vg.Spec.DeviceSelector.Paths)
				copy(originalOptPaths, vg.Spec.DeviceSelector.OptionalPaths)
			}

			err := DefaultFilters(context.Background(), &Options{VG: vg})[partOfDeviceSelector](tc.device, tracingResolver)
			tc.assertErr(t, err, fmt.Sprintf("partOfDeviceSelector(%v)", tc.device))

			if vg.Spec.DeviceSelector != nil {
				assert.ElementsMatch(t, originalPaths, vg.Spec.DeviceSelector.Paths,
					"device selector Paths should not be mutated")
				assert.ElementsMatch(t, originalOptPaths, vg.Spec.DeviceSelector.OptionalPaths,
					"device selector OptionalPaths should not be mutated")
			}

			for _, call := range resolverCalls {
				assert.False(t, lvmv1alpha1.DevicePath(call).IsRegex(),
					"resolver should not be called for regex paths: %s", call)
			}
		})
	}

	t.Run("shared selector unchanged across multiple device discoveries", func(t *testing.T) {
		sharedSelector := &lvmv1alpha1.DeviceSelector{
			Paths:         []lvmv1alpha1.DevicePath{"regex:/dev/nvme[0-9]+n1"},
			OptionalPaths: []lvmv1alpha1.DevicePath{"/dev/sda"},
		}

		vg1 := &lvmv1alpha1.LVMVolumeGroup{}
		vg1.SetName("vg1")
		vg1.Spec.DeviceSelector = sharedSelector

		originalPaths1 := make([]lvmv1alpha1.DevicePath, len(sharedSelector.Paths))
		copy(originalPaths1, sharedSelector.Paths)
		originalOptPaths1 := make([]lvmv1alpha1.DevicePath, len(sharedSelector.OptionalPaths))
		copy(originalOptPaths1, sharedSelector.OptionalPaths)

		device1 := lsblk.BlockDevice{KName: "/dev/nvme0n1"}
		resolverCalls1 := []string{}
		resolver1 := symlinkResolver.NewWithResolver(func(path string) (string, error) {
			resolverCalls1 = append(resolverCalls1, string(path))
			return path, nil
		})

		err1 := DefaultFilters(context.Background(), &Options{VG: vg1})[partOfDeviceSelector](device1, resolver1)
		assert.NoError(t, err1)
		assert.Equal(t, originalPaths1, vg1.Spec.DeviceSelector.Paths)
		assert.Equal(t, originalOptPaths1, vg1.Spec.DeviceSelector.OptionalPaths)
		assert.Equal(t, []string{}, resolverCalls1)

		vg2 := &lvmv1alpha1.LVMVolumeGroup{}
		vg2.SetName("vg2")
		vg2.Spec.DeviceSelector = sharedSelector

		originalPaths2 := make([]lvmv1alpha1.DevicePath, len(sharedSelector.Paths))
		copy(originalPaths2, sharedSelector.Paths)
		originalOptPaths2 := make([]lvmv1alpha1.DevicePath, len(sharedSelector.OptionalPaths))
		copy(originalOptPaths2, sharedSelector.OptionalPaths)

		device2 := lsblk.BlockDevice{KName: "/dev/nvme10n1"}
		resolverCalls2 := []string{}
		resolver2 := symlinkResolver.NewWithResolver(func(path string) (string, error) {
			resolverCalls2 = append(resolverCalls2, string(path))
			return path, nil
		})

		err2 := DefaultFilters(context.Background(), &Options{VG: vg2})[partOfDeviceSelector](device2, resolver2)
		assert.NoError(t, err2)
		assert.Equal(t, originalPaths2, vg2.Spec.DeviceSelector.Paths)
		assert.Equal(t, originalOptPaths2, vg2.Spec.DeviceSelector.OptionalPaths)
		assert.Equal(t, []string{}, resolverCalls2)

		assert.Equal(t, []lvmv1alpha1.DevicePath{"regex:/dev/nvme[0-9]+n1"}, sharedSelector.Paths)
		assert.Equal(t, []lvmv1alpha1.DevicePath{"/dev/sda"}, sharedSelector.OptionalPaths)
	})
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
