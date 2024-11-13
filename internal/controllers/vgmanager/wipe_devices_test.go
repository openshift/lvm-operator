package vgmanager

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/internal/controllers/constants"
	dmsetupmocks "github.com/openshift/lvm-operator/internal/controllers/vgmanager/dmsetup/mocks"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lsblk"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvm"
	wipefsmocks "github.com/openshift/lvm-operator/internal/controllers/vgmanager/wipefs/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"sigs.k8s.io/controller-runtime/pkg/log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestWipeDevices(t *testing.T) {
	tests := []struct {
		name                 string
		forceWipeNotEnabled  bool
		devicePaths          []string
		optionalDevicePaths  []string
		blockDevices         []lsblk.BlockDevice
		vgs                  []lvm.VolumeGroup
		wipeCount            int
		removeReferenceCount int
		wipedBefore          bool
	}{
		{
			name:                 "Force wipe feature is not enabled",
			forceWipeNotEnabled:  true,
			devicePaths:          []string{"/dev/loop1"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/sdb"}, {KName: "/dev/loop1"}},
			wipeCount:            0,
			removeReferenceCount: 0,
		},
		{
			name:                 "There is no path specified",
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/sdb"}, {KName: "/dev/loop1"}},
			wipeCount:            0,
			removeReferenceCount: 0,
		},
		{
			name:                 "Device exist in the device list",
			devicePaths:          []string{"/dev/loop1"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/sdb"}, {KName: "/dev/loop1"}},
			vgs:                  []lvm.VolumeGroup{{Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/sda"}}}},
			wipeCount:            1,
			removeReferenceCount: 0,
		},
		{
			name:                 "Device does not exist in the device list",
			devicePaths:          []string{"/dev/loop1"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/sdb"}},
			wipeCount:            0,
			removeReferenceCount: 0,
		},
		{
			name:                 "Device exist as a child",
			devicePaths:          []string{"/dev/loop1"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda", Children: []lsblk.BlockDevice{{KName: "/dev/loop1"}}}, {KName: "/dev/sdb"}},
			wipeCount:            1,
			removeReferenceCount: 0,
		},
		{
			name:                 "Device has child references",
			devicePaths:          []string{"/dev/loop1"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/loop1", Children: []lsblk.BlockDevice{{KName: "/dev/loop1p1", Children: []lsblk.BlockDevice{{KName: "/dev/loop1p1p1"}}}, {KName: "/dev/loop1p2"}}}, {KName: "/dev/sda"}},
			wipeCount:            1,
			removeReferenceCount: 3,
		},
		{
			name:                 "Device exists as a child device that has a child reference",
			devicePaths:          []string{"/dev/loop1"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda", Children: []lsblk.BlockDevice{{KName: "/dev/loop1", Children: []lsblk.BlockDevice{{KName: "/dev/loop1p1"}}}, {KName: "/dev/loop2", Children: []lsblk.BlockDevice{{KName: "/dev/loop2p1"}}}}}, {KName: "/dev/sdb"}},
			wipeCount:            1,
			removeReferenceCount: 1,
		},
		{
			name:                 "Device exist in the device list and is already part of a vg",
			devicePaths:          []string{"/dev/loop1"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/sdb"}, {KName: "/dev/loop1"}},
			vgs:                  []lvm.VolumeGroup{{Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/loop1"}}}},
			wipeCount:            1,
			removeReferenceCount: 0,
		},
		{
			name:                 "Device exist in the device list and is already part of a vg, but was already wiped before",
			devicePaths:          []string{"/dev/loop1"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/sdb"}, {KName: "/dev/loop1"}},
			vgs:                  []lvm.VolumeGroup{{Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/loop1"}}}},
			wipedBefore:          true,
			wipeCount:            0,
			removeReferenceCount: 0,
		},
		{
			name:                 "Only one device out of two exists in the device list",
			devicePaths:          []string{"/dev/loop1", "/dev/loop2"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/sdb"}, {KName: "/dev/loop1"}},
			vgs:                  []lvm.VolumeGroup{{Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/sda"}}}},
			wipeCount:            1,
			removeReferenceCount: 0,
		},
		{
			name:                 "Both devices exist in the device list",
			devicePaths:          []string{"/dev/loop1", "/dev/loop2"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/loop1"}, {KName: "/dev/loop2", Children: []lsblk.BlockDevice{{KName: "/dev/loop2p1"}}}},
			vgs:                  []lvm.VolumeGroup{{Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/sda"}}}},
			wipeCount:            2,
			removeReferenceCount: 1,
		},
		{
			name:                 "One required and one optional device exist in the device list",
			devicePaths:          []string{"/dev/loop1"},
			optionalDevicePaths:  []string{"/dev/loop2"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/loop1"}, {KName: "/dev/loop2", Children: []lsblk.BlockDevice{{KName: "/dev/loop2p1"}}}},
			vgs:                  []lvm.VolumeGroup{{Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/sda"}}}},
			wipeCount:            2,
			removeReferenceCount: 1,
		},
		{
			name:                 "Optional device does not exist in the device list",
			devicePaths:          []string{"/dev/loop1"},
			optionalDevicePaths:  []string{"/dev/loop2"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/loop1"}},
			vgs:                  []lvm.VolumeGroup{{Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/sda"}}}},
			wipeCount:            1,
			removeReferenceCount: 0,
		},
		{
			name:                 "Both devices, one of them is a child, exist in the device list",
			devicePaths:          []string{"/dev/loop1", "/dev/loop2p1"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/loop1"}, {KName: "/dev/loop2", Children: []lsblk.BlockDevice{{KName: "/dev/loop2p1"}}}},
			vgs:                  []lvm.VolumeGroup{{Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/sda"}}}},
			wipeCount:            2,
			removeReferenceCount: 0,
		},
		{
			name:                 "Both devices exist in the device list, one of them is part of the vg",
			devicePaths:          []string{"/dev/loop1", "/dev/loop2"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/loop1"}, {KName: "/dev/loop2", Children: []lsblk.BlockDevice{{KName: "/dev/loop2p1"}}}},
			vgs:                  []lvm.VolumeGroup{{Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/loop1"}}}},
			wipeCount:            2,
			removeReferenceCount: 1,
		},
		{
			name:                 "Both devices are part of the vg, one of them with a child",
			devicePaths:          []string{"/dev/loop1", "/dev/loop2"},
			blockDevices:         []lsblk.BlockDevice{{KName: "/dev/sda"}, {KName: "/dev/loop1"}, {KName: "/dev/loop2", Children: []lsblk.BlockDevice{{KName: "/dev/loop2p1"}}}},
			vgs:                  []lvm.VolumeGroup{{Name: "vg1", PVs: []lvm.PhysicalVolume{{PvName: "/dev/loop1"}, {PvName: "/dev/loop2"}}}},
			wipeCount:            2,
			removeReferenceCount: 1,
		},
	}
	mockWipefs := wipefsmocks.NewMockWipefs(t)
	mockDmsetup := dmsetupmocks.NewMockDmsetup(t)
	evalSymlinks = func(path string) (string, error) {
		return path, nil
	}
	defer func() {
		evalSymlinks = filepath.EvalSymlinks
	}()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			r := &Reconciler{NodeName: "test", Wipefs: mockWipefs, Dmsetup: mockDmsetup}
			if tt.wipeCount > 0 {
				mockWipefs.EXPECT().Wipe(ctx, mock.Anything).Return(nil).Times(tt.wipeCount)
			}
			if tt.removeReferenceCount > 0 {
				mockDmsetup.EXPECT().Remove(ctx, mock.Anything).Return(nil).Times(tt.removeReferenceCount)
			}
			volumeGroup := &v1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "vg1"},
				Spec: v1alpha1.LVMVolumeGroupSpec{DeviceSelector: &v1alpha1.DeviceSelector{
					Paths:                             tt.devicePaths,
					OptionalPaths:                     tt.optionalDevicePaths,
					ForceWipeDevicesAndDestroyAllData: ptr.To[bool](!tt.forceWipeNotEnabled),
				}},
			}

			if tt.wipedBefore {
				volumeGroup.Annotations = map[string]string{
					constants.DevicesWipedAnnotationPrefix + r.NodeName: time.Now().Format(time.RFC3339)}
			}

			wiped, err := r.wipeDevices(ctx, volumeGroup, tt.blockDevices)
			if tt.wipeCount > 0 {
				assert.True(t, wiped)
			} else {
				assert.False(t, wiped)
			}
			assert.NoError(t, err)
		})
	}
}
