package vgmanager

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/pkg/internal"
	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var devicePaths map[string]string

func TestAvailableDevicesForVG(t *testing.T) {
	// create a folder for each disk to resolve filepath.EvalSymlinks(path) call in filterMatchingDevices.
	tmpDir := t.TempDir()
	devicePaths = make(map[string]string)
	devicePaths["nvme1n1p1"] = fmt.Sprintf("%s/%s", tmpDir, "nvme1n1p1")
	devicePaths["nvme1n1p2"] = fmt.Sprintf("%s/%s", tmpDir, "nvme1n1p2")
	for _, path := range devicePaths {
		err := os.Mkdir(path, 0755)
		if err != nil {
			t.Fatal(err)
		}
	}

	r := &VGReconciler{}

	// remove noBindMounts filter as it reads `proc/1/mountinfo` file.
	delete(FilterMap, "noBindMounts")

	testCases := []struct {
		description           string
		volumeGroup           v1alpha1.LVMVolumeGroup
		existingBlockDevices  []internal.BlockDevice
		existingVGs           []VolumeGroup
		numOfAvailableDevices int
		expectError           bool
	}{
		{
			description: "device is available to use",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
			},
			existingBlockDevices: []internal.BlockDevice{
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
			existingBlockDevices: []internal.BlockDevice{
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
			existingBlockDevices: []internal.BlockDevice{
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
			existingBlockDevices: []internal.BlockDevice{
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
			existingBlockDevices: []internal.BlockDevice{
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
			existingBlockDevices: []internal.BlockDevice{
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
			existingBlockDevices: []internal.BlockDevice{
				{
					Name:     "/dev/nvme1n1",
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					KName:    "/dev/nvme1n1",
					Children: []internal.BlockDevice{
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
			existingBlockDevices: []internal.BlockDevice{
				{
					Name:     "/dev/nvme1n1",
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					KName:    "/dev/nvme1n1",
					Children: []internal.BlockDevice{
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
						Paths: []string{
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			existingBlockDevices: []internal.BlockDevice{
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
						Paths: []string{
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			existingBlockDevices:  []internal.BlockDevice{},
			numOfAvailableDevices: 0,
			expectError:           true,
		},
		{
			description: "vg has device path that exists but read-only",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []string{
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			existingBlockDevices: []internal.BlockDevice{
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
			description: "vg has device path that is already a part of the existing vg",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []string{
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			existingVGs: []VolumeGroup{
				{
					Name: "vg1",
					PVs: []string{
						calculateDevicePath(t, "nvme1n1p1"),
					},
				},
			},
			existingBlockDevices: []internal.BlockDevice{
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
						Paths: []string{
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			existingVGs: []VolumeGroup{
				{
					Name: "vg1",
				},
			},
			existingBlockDevices: []internal.BlockDevice{
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
						Paths: []string{
							devicePaths["nvme1n1p2"],
						},
					},
				},
			},
			existingVGs: []VolumeGroup{
				{
					Name: "vg1",
				},
			},
			existingBlockDevices: []internal.BlockDevice{
				{
					Name:     "nvme1n1p1",
					KName:    calculateDevicePath(t, "nvme1n1p1"),
					Type:     "disk",
					Size:     "279.4G",
					ReadOnly: false,
					State:    "live",
					Children: []internal.BlockDevice{
						{
							Name:     "nvme1n1p2",
							KName:    calculateDevicePath(t, "nvme1n1p2"),
							Type:     "disk",
							Size:     "4G",
							ReadOnly: false,
							State:    "live",
						},
					},
				},
			},
			numOfAvailableDevices: 1,
			expectError:           false,
		},
		{
			description: "vg has required and optional devices",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []string{
							devicePaths["nvme1n1p1"],
						},
						OptionalPaths: []string{
							devicePaths["nvme1n1p2"],
						},
					},
				},
			},
			existingBlockDevices: []internal.BlockDevice{
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
						OptionalPaths: []string{
							devicePaths["nvme1n1p1"],
							devicePaths["nvme1n1p2"],
						},
					},
				},
			},
			existingBlockDevices: []internal.BlockDevice{
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
						OptionalPaths: []string{
							devicePaths["nvme1n1p2"],
						},
					},
				},
			},
			existingBlockDevices: []internal.BlockDevice{
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
						Paths: []string{
							devicePaths["nvme1n1p1"],
						},
						OptionalPaths: []string{
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			numOfAvailableDevices: 0,
			expectError:           true,
		},
		{
			description: "vg has duplicate required device paths listed",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						Paths: []string{
							devicePaths["nvme1n1p1"],
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			numOfAvailableDevices: 0,
			expectError:           true,
		},
		{
			description: "vg has duplicate optional device paths listed",
			volumeGroup: v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: v1alpha1.LVMVolumeGroupSpec{
					DeviceSelector: &v1alpha1.DeviceSelector{
						OptionalPaths: []string{
							devicePaths["nvme1n1p1"],
							devicePaths["nvme1n1p1"],
						},
					},
				},
			},
			numOfAvailableDevices: 0,
			expectError:           true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			availableDevices, err := r.getAvailableDevicesForVG(tc.existingBlockDevices, tc.existingVGs, &tc.volumeGroup)
			if !tc.expectError {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
			assert.Equal(t, tc.numOfAvailableDevices, len(availableDevices), "expected numOfAvailableDevices is not equal to actual number")
		})
	}
}

// calculateDevicePath calculates the device path to be used in KNames.
// it has /private in the beginning because /tmp symlinks are evaluated as with /private in the beginning on darwin.
func calculateDevicePath(t *testing.T, deviceName string) string {
	t.Helper()
	if runtime.GOOS == "darwin" {
		return fmt.Sprintf("/private%s", devicePaths[deviceName])
	}
	return devicePaths[deviceName]
}
