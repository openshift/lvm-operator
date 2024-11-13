package vgmanager

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var devicePaths map[string]v1alpha1.DevicePath

func Test_getNewDevicesToBeAdded(t *testing.T) {
	// create a folder for each disk to resolve filepath.EvalSymlinks(path) call in getDeviceCandidates.
	tmpDir := t.TempDir()
	devicePaths = make(map[string]v1alpha1.DevicePath)
	devicePaths["nvme1n1p1"] = v1alpha1.DevicePath(fmt.Sprintf("%s/%s", tmpDir, "nvme1n1p1"))
	devicePaths["nvme1n1p2"] = v1alpha1.DevicePath(fmt.Sprintf("%s/%s", tmpDir, "nvme1n1p2"))
	devicePaths["md1"] = v1alpha1.DevicePath(fmt.Sprintf("%s/%s", tmpDir, "md1"))
	for _, path := range devicePaths {
		err := os.Mkdir(path.Unresolved(), 0755)
		if err != nil {
			t.Fatal(err)
		}
	}

	testCases := []struct {
		description           string
		volumeGroup           v1alpha1.LVMVolumeGroup
		existingBlockDevices  []lsblk.BlockDevice
		numOfAvailableDevices int
		expectError           bool
		vgs                   []lvm.VolumeGroup
	}{
		{
			description: "device is available to use",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "/dev/nvme1n1",
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					KName:    "/dev/nvme1n1",
				},
			},
			numOfAvailableDevices: 1,
		},
		{
			description: "device is read-only",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "/dev/nvme1n1",
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: true,
					State:    "live",
					KName:    "/dev/nvme1n1",
				},
			},
			numOfAvailableDevices: 0,
		},
		{
			description: "device is suspended",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "/dev/nvme1n1",
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "suspended",
					KName:    "/dev/nvme1n1",
				},
			},
			numOfAvailableDevices: 0,
		},
		{
			description: "device has bios-boot partlabel",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:      "/dev/nvme1n1",
					Type:      "disk",
					Size:      "279.4G",
					ReadOnly:  false,
					State:     "live",
					KName:     "/dev/nvme1n1",
					PartLabel: "BIOS-BOOT",
				},
			},
			numOfAvailableDevices: 0,
		},
		{
			description: "device has reserved partlabel",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:      "/dev/nvme1n1",
					Type:      "disk",
					Size:      "279.4G",
					ReadOnly:  false,
					State:     "live",
					KName:     "/dev/nvme1n1",
					PartLabel: "reserved",
				},
			},
			numOfAvailableDevices: 0,
		},
		{
			description: "device has filesystem signature",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "/dev/nvme1n1",
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					KName:    "/dev/nvme1n1",
					FSType:   "ext4",
				},
			},
			numOfAvailableDevices: 0,
		},
		{
			description: "device has children",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "/dev/nvme1n1",
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					KName:    "/dev/nvme1n1",
					Children: []lsblk.BlockDevice{
						{
							Name:     "/dev/nvme1n1p1",
							ReadOnly: true,
						},
					},
				},
			},
			numOfAvailableDevices: 0,
		},
		{
			description: "device has children that are available",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "/dev/nvme1n1",
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					KName:    "/dev/nvme1n1",
					Children: []lsblk.BlockDevice{
						{
							Name:     "/dev/nvme1n1p1",
							Type:     "disk",
							Size:     "50G",
							ReadOnly: false,
							State:    "live",
							KName:    "/dev/nvme1n1p1",
						},
						{
							Name:     "/dev/nvme1n1p2",
							Type:     "disk",
							Size:     "50G",
							ReadOnly: false,
							State:    "live",
							KName:    "/dev/nvme1n1p2",
						},
					},
				},
			},
			numOfAvailableDevices: 2,
		},
		{
			description: "vg has device path that is available in block devices",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "nvme1n1p1",
					KName:    calculateDevicePath(t, "nvme1n1p1"),
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
				},
			},
			numOfAvailableDevices: 1,
		},
		{
			description: "vg has device path that does not exist in block devices",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			existingBlockDevices:  []lsblk.BlockDevice{},
			numOfAvailableDevices: 0,
		},
		{
			description: "vg has device path that exists but read-only",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "nvme1n1p1",
					KName:    calculateDevicePath(t, "nvme1n1p1"),
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: true,
					State:    "live",
				},
			},
			numOfAvailableDevices: 0,
		},
		{
			description: "vg has device paths that are already a part of the existing vg",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p1"],
						},
						OptionalPaths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p2"],
						},
					},
				},
			},
			vgs: []lvm.VolumeGroup{
				{
					Name: "vg1",
					PVs: []lvm.PhysicalVolume{
						{
							PvName: calculateDevicePath(t, "nvme1n1p1"),
							VgName: "vg1",
						},
						{
							PvName: calculateDevicePath(t, "nvme1n1p2"),
							VgName: "vg1",
						},
					},
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "nvme1n1p1",
					KName:    calculateDevicePath(t, "nvme1n1p1"),
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					FSType:   filter.FSTypeLVM2Member,
				},
				{
					Name:     "nvme1n1p2",
					KName:    calculateDevicePath(t, "nvme1n1p2"),
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					FSType:   filter.FSTypeLVM2Member,
				},
			},
			numOfAvailableDevices: 0,
			expectError:           false,
		},
		{
			description: "vg has device path that is not a part of the existing vg",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			vgs: []lvm.VolumeGroup{
				{
					Name: "vg1",
					PVs: []lvm.PhysicalVolume{
						{
							PvName: calculateDevicePath(t, "nvme1n1p2"),
							VgName: "vg1",
						},
					},
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "nvme1n1p1",
					KName:    calculateDevicePath(t, "nvme1n1p1"),
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
				},
				{
					Name:     "nvme1n1p2",
					KName:    calculateDevicePath(t, "nvme1n1p2"),
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					FSType:   filter.FSTypeLVM2Member,
				},
			},
			numOfAvailableDevices: 1,
			expectError:           false,
		},
		{
			description: "vg has device path that is a child disk and is not a part of the existing vg",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p2"],
						},
					},
				},
			},
			vgs: []lvm.VolumeGroup{
				{
					Name: "vg1",
					PVs: []lvm.PhysicalVolume{
						{
							PvName: calculateDevicePath(t, "nvme1n1p2"),
							VgName: "vg1",
						},
					},
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "nvme1n1p1",
					KName:    calculateDevicePath(t, "nvme1n1p1"),
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					Children: []lsblk.BlockDevice{
						{
							Name:     "nvme1n1p2",
							KName:    calculateDevicePath(t, "nvme1n1p2"),
							Type:     "disk",
							Size:     "4G",
							ReadOnly: false,
							State:    "live",
							FSType:   filter.FSTypeLVM2Member,
						},
					},
				},
			},
			numOfAvailableDevices: 0,
		},
		{
			description: "vg has required and optional devices",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p1"],
						},
						OptionalPaths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p2"],
						},
					},
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "nvme1n1p1",
					KName:    calculateDevicePath(t, "nvme1n1p1"),
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					FSType:   filter.FSTypeLVM2Member,
				},
				{
					Name:     "nvme1n1p2",
					KName:    calculateDevicePath(t, "nvme1n1p2"),
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					FSType:   filter.FSTypeLVM2Member,
				},
			},
			numOfAvailableDevices: 2,
			expectError:           false,
		},
		{
			description: "vg has an optional devices and no required devices",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						OptionalPaths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p1"],
							devicePaths["nvme1n1p2"],
						},
					},
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "nvme1n1p1",
					KName:    calculateDevicePath(t, "nvme1n1p1"),
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					FSType:   filter.FSTypeLVM2Member,
				},
			},
			numOfAvailableDevices: 1,
			expectError:           false,
		},
		{
			description: "vg has no required devices and no available optional devices",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						OptionalPaths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p2"],
						},
					},
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "nvme1n1p1",
					KName:    calculateDevicePath(t, "nvme1n1p1"),
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
				},
			},
			numOfAvailableDevices: 0,
			expectError:           true,
		},
		{
			description: "vg has duplicate required and optional device paths listed",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p1"],
						},
						OptionalPaths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			numOfAvailableDevices: 0,
		},
		{
			description: "vg has duplicate required device paths listed",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p1"],
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			numOfAvailableDevices: 0,
		},
		{
			description: "vg has duplicate optional device paths listed",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						OptionalPaths: []v1alpha1.DevicePath{
							devicePaths["nvme1n1p1"],
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			numOfAvailableDevices: 0,
		},
		{
			description: "vg RAID with multiple paths under different devices",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []v1alpha1.DevicePath{
							devicePaths["md1"],
						},
					},
				},
			},
			existingBlockDevices: []lsblk.BlockDevice{
				{
					Name:     "nvme1n1p1",
					Type:     "disk",
					Size:     "50G",
					ReadOnly: false,
					State:    "live",
					KName:    calculateDevicePath(t, "nvme1n1p1"),
					Serial:   "vol071204a42d92d52a2",
					FSType:   "linux_raid_member",
					Children: []lsblk.BlockDevice{{
						Name:     "/dev/md1",
						Type:     "raid1",
						Size:     "50G",
						ReadOnly: false,
						KName:    calculateDevicePath(t, "md1"),
					}},
				},
				{
					Name:     "nvme1n1p2",
					Type:     "disk",
					Size:     "50G",
					ReadOnly: false,
					State:    "live",
					KName:    calculateDevicePath(t, "nvme1n1p2"),
					Serial:   "vol07993aa64fe3c5316",
					FSType:   "linux_raid_member",
					Children: []lsblk.BlockDevice{{
						Name:     "/dev/md1",
						Type:     "raid1",
						Size:     "50G",
						ReadOnly: false,
						KName:    calculateDevicePath(t, "md1"),
					}},
				},
			},
			numOfAvailableDevices: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))

			opts := &filter.Options{
				VG:  &tc.volumeGroup,
				BDI: nil,
				PVs: []lvm.PhysicalVolume{},
			}
			for _, vg := range tc.vgs {
				opts.PVs = append(opts.PVs, vg.PVs...)
			}
			filters := filter.DefaultFilters(context.Background(), opts)

			devices := filterDevices(ctx, tc.existingBlockDevices, symlinkResolver.NewWithDefaultResolver(), filters)
			assert.Equal(t, tc.numOfAvailableDevices, len(devices.Available), "expected numOfAvailableDevices is not equal to actual number")

			if tc.expectError {
				if len(devices.Excluded) == 0 {
					t.Errorf("expected error but got no excluded devices which could have signalled an error")
				}
			}
		})
	}
}

// calculateDevicePath calculates the device path to be used in KNames.
// it has /private in the beginning because /tmp symlinks are evaluated as with /private in the beginning on darwin.
func calculateDevicePath(t *testing.T, deviceName string) string {
	t.Helper()
	return getKNameFromDevice(devicePaths[deviceName].Unresolved()).Unresolved()
}
