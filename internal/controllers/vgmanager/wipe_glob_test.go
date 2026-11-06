package vgmanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	symlinkResolver "github.com/openshift/lvm-operator/v4/internal/controllers/symlink-resolver"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk"
	lsblkmocks "github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lsblk/mocks"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	lvmmocks "github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm/mocks"
	wipefsmocks "github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/wipefs/mocks"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testWipeDisk(name, number string) lsblk.BlockDevice {
	mounts := []*string{nil}
	return lsblk.BlockDevice{Name: name, KName: name, Type: "disk", Size: "100G", MajorMinor: number, Serial: "serial-" + number, Mountpoints: &mounts}
}

// Model a stale informer cache: a wipe must verify its marker through APIReader.
type rejectCachedGetClient struct{ client.Client }

func (c rejectCachedGetClient) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return errors.New("cached read must not be used for wipe verification")
}

func TestGlobWipeDeviceSafety(t *testing.T) {
	base := testWipeDisk("/dev/sda", "8:0")
	for _, tc := range []struct {
		name   string
		modify func(*lsblk.BlockDevice)
		want   string
	}{
		{name: "safe", modify: func(*lsblk.BlockDevice) {}},
		{name: "unknown mounts", modify: func(d *lsblk.BlockDevice) { d.Mountpoints = nil }, want: "mount state"},
		{name: "mounted", modify: func(d *lsblk.BlockDevice) { d.Mountpoints = ptr.To([]*string{ptr.To("/data")}) }, want: "mounted"},
		{name: "active swap", modify: func(d *lsblk.BlockDevice) { d.Mountpoints = ptr.To([]*string{ptr.To("[SWAP]")}) }, want: "swap"},
		{name: "filesystem swap", modify: func(d *lsblk.BlockDevice) { d.FSType = "swap" }, want: "swap"},
		{name: "hidden lvm", modify: func(d *lsblk.BlockDevice) { d.FSType = "LVM2_member" }, want: "LVM2_member"},
		{name: "raid", modify: func(d *lsblk.BlockDevice) { d.FSType = "linux_raid_member" }, want: "linux_raid_member"},
		// A closed LUKS volume has no children, no mountpoint and reports as a plain
		// device, so this check is all that stands between its header and wipefs.
		{name: "luks", modify: func(d *lsblk.BlockDevice) { d.FSType = "crypto_LUKS" }, want: "crypto_LUKS"},
		// The selection filter accepts these, so refusing them here excluded them from
		// the volume group entirely rather than merely skipping the wipe.
		{name: "loop device", modify: func(d *lsblk.BlockDevice) { d.Type = "loop" }},
		{name: "multipath device", modify: func(d *lsblk.BlockDevice) { d.Type = "mpath" }},
		{name: "rom is still refused", modify: func(d *lsblk.BlockDevice) { d.Type = "rom" }, want: "unsupported device type"},
		{name: "children", modify: func(d *lsblk.BlockDevice) { d.Children = []lsblk.BlockDevice{{KName: "/dev/sda1"}} }, want: "dependent"},
		{name: "no device number", modify: func(d *lsblk.BlockDevice) { d.MajorMinor = "" }, want: "device number"},
		{name: "no hardware id", modify: func(d *lsblk.BlockDevice) { d.Serial = "" }},
		{name: "read-only", modify: func(d *lsblk.BlockDevice) { d.ReadOnly = true }, want: "read-only"},
		{name: "boot partition", modify: func(d *lsblk.BlockDevice) { d.PartLabel = "BIOS boot" }, want: "partition label"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device := base
			tc.modify(&device)
			_, err := globWipeDevice(device.KName, []lsblk.BlockDevice{device}, nil)
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
		})
	}
	parent := testWipeDisk("/dev/sda", "8:0")
	parent.Mountpoints = ptr.To([]*string{ptr.To("/host")})
	child := testWipeDisk("/dev/sda1", "8:1")
	child.Type = "part"
	parent.Children = []lsblk.BlockDevice{child}
	_, err := globWipeDevice(child.KName, []lsblk.BlockDevice{parent}, nil)
	require.ErrorContains(t, err, "mounted")
}

func TestGlobWipeExclusionsPersistDuringDiscovery(t *testing.T) {
	safe := testWipeDisk("/dev/sda", "8:0")
	unknown := testWipeDisk("/dev/sdb", "8:16")
	unknown.Serial = ""
	parent := testWipeDisk("/dev/sdc", "8:32")
	parent.Mountpoints = ptr.To([]*string{ptr.To("/host")})
	child := testWipeDisk("/dev/sdc1", "8:33")
	child.Type = "part"
	parent.Children = []lsblk.BlockDevice{child}
	filtered := filterGlobWipeCandidates(FilteredBlockDevices{Available: []lsblk.BlockDevice{safe, unknown, child}}, []lsblk.BlockDevice{safe, unknown, parent}, nil)
	require.Equal(t, []lsblk.BlockDevice{safe, unknown}, filtered.Available)
	require.ErrorContains(t, filtered.FilterErrors("/dev/sdc1")[0], "mounted")
}

func TestWipeUnassignedPVWithoutFilesystemSignature(t *testing.T) {
	for _, selection := range []string{"required glob", "optional glob", "literal"} {
		t.Run(selection, func(t *testing.T) {
			path, resolver := globDeviceFixture(t)
			vg := &v1alpha1.LVMVolumeGroup{}
			vg.Name = "vg1"
			vg.Spec.DeviceSelector = &v1alpha1.DeviceSelector{
				Paths: []v1alpha1.DevicePath{path}, ForceWipeDevicesAndDestroyAllData: ptr.To(true),
			}
			switch selection {
			case "optional glob":
				vg.Spec.DeviceSelector.OptionalPaths, vg.Spec.DeviceSelector.Paths = vg.Spec.DeviceSelector.Paths, nil
			case "literal":
				vg.Spec.DeviceSelector.Paths = []v1alpha1.DevicePath{"/dev/sda"}
			}
			ctx := context.Background()
			wipeClient := wipefsmocks.NewMockWipefs(t)
			if selection == "literal" {
				wipeClient.EXPECT().Wipe(ctx, "/dev/sda").Return(nil).Once()
			}
			r := &Reconciler{NodeName: "node", Wipefs: wipeClient}
			wiped, err := r.wipeDevices(ctx, vg, []lsblk.BlockDevice{testWipeDisk("/dev/sda", "8:0")}, []lvm.PhysicalVolume{{PvName: "/dev/sda"}}, resolver)
			if selection == "required glob" {
				require.ErrorContains(t, err, "no safe devices to wipe")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, selection == "literal", wiped)
			if selection != "literal" {
				require.Empty(t, vg.Annotations)
				wipeClient.AssertNotCalled(t, "Wipe", ctx, "/dev/sda")
			}
		})
	}
}

func TestGlobWipeRechecksAndPersistsMarker(t *testing.T) {
	for _, tc := range []struct {
		name         string
		change       string
		noHardwareID bool
		updateFails  bool
	}{
		{name: "stable devices are wiped"},
		{name: "devices without hardware identifiers are wiped", noHardwareID: true},
		{name: "device becomes mounted", change: "mounted"},
		{name: "device identity changes", change: "identity"},
		{name: "device WWN changes", change: "wwn"},
		{name: "device disappears", change: "missing"},
		{name: "device becomes a physical volume", change: "owned"},
		{name: "device becomes an unassigned physical volume without an lsblk signature", change: "unassigned"},
		{name: "hidden LVM signature appears", change: "signature"},
		{name: "selector changes", change: "configuration"},
		{name: "marker cannot be persisted", updateFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, resolver := globDeviceFixture(t)
			ctx := context.Background()
			vg := &v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "test"}, Spec: v1alpha1.LVMVolumeGroupSpec{DeviceSelector: &v1alpha1.DeviceSelector{
				Paths: []v1alpha1.DevicePath{path}, ForceWipeDevicesAndDestroyAllData: ptr.To(true),
			}}}
			scheme := runtime.NewScheme()
			require.NoError(t, v1alpha1.AddToScheme(scheme))
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if !tc.updateFails {
				builder = builder.WithObjects(vg.DeepCopy())
			}
			k8sClient := builder.Build()
			if !tc.updateFails {
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(vg), vg))
			}
			blockDevices := []lsblk.BlockDevice{testWipeDisk("/dev/sda", "8:0"), testWipeDisk("/dev/sdb", "8:16")}
			if tc.noHardwareID {
				for i := range blockDevices {
					blockDevices[i].Serial = ""
				}
			}
			if tc.change == "wwn" {
				blockDevices[0].WWN = "original-wwn"
			}
			fresh := append([]lsblk.BlockDevice(nil), blockDevices...)
			var freshPVs []lvm.PhysicalVolume
			switch tc.change {
			case "mounted":
				fresh[0].Mountpoints = ptr.To([]*string{ptr.To("/data")})
			case "identity":
				fresh[0].Serial = "different-device"
			case "wwn":
				fresh[0].WWN = "different-wwn"
			case "missing":
				fresh = fresh[1:]
			case "owned":
				freshPVs = []lvm.PhysicalVolume{{PvName: "/dev/sda", VgName: "other"}}
			case "unassigned":
				freshPVs = []lvm.PhysicalVolume{{PvName: "/dev/sda"}}
			case "signature":
				fresh[0].FSType = "LVM2_member"
			}
			lsblkClient := lsblkmocks.NewMockLSBLK(t)
			lvmClient := lvmmocks.NewMockLVM(t)
			wipeClient := wipefsmocks.NewMockWipefs(t)
			if !tc.updateFails {
				count := 2
				if tc.change != "" {
					count = 1
				}
				if tc.change == "configuration" {
					lsblkClient.EXPECT().ListBlockDevices(ctx).RunAndReturn(func(context.Context) ([]lsblk.BlockDevice, error) {
						current := &v1alpha1.LVMVolumeGroup{}
						require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(vg), current))
						current.Spec.DeviceSelector.Paths = []v1alpha1.DevicePath{"/dev/sdb"}
						require.NoError(t, k8sClient.Update(ctx, current))
						return fresh, nil
					}).Once()
				} else {
					lsblkClient.EXPECT().ListBlockDevices(ctx).Return(fresh, nil).Times(count)
				}
				lvmClient.EXPECT().ListPVs(ctx, "").Return(freshPVs, nil).Times(count)
				if tc.change == "" {
					wipeClient.EXPECT().Wipe(ctx, "/dev/sda").Return(nil).Once()
					wipeClient.EXPECT().Wipe(ctx, "/dev/sdb").Return(nil).Once()
				}
			}
			r := &Reconciler{Client: rejectCachedGetClient{k8sClient}, APIReader: k8sClient, NodeName: "node", LSBLK: lsblkClient, LVM: lvmClient, Wipefs: wipeClient}
			wiped, err := r.wipeDevices(ctx, vg, blockDevices, nil, resolver)
			if tc.updateFails {
				require.ErrorContains(t, err, "persist wipe marker")
				require.False(t, wiped)
				return
			}
			stored := &v1alpha1.LVMVolumeGroup{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(vg), stored))
			require.Contains(t, stored.Annotations, constants.DevicesWipedAnnotationPrefix+"node")
			if tc.change != "" {
				switch tc.change {
				case "configuration":
					require.ErrorContains(t, err, "configuration or wipe marker changed")
				case "identity", "wwn":
					require.ErrorContains(t, err, "changed identity")
				default:
					require.ErrorContains(t, err, "no longer safe")
				}
				require.False(t, wiped)
				wipeClient.AssertNotCalled(t, "Wipe", ctx, "/dev/sda")
			} else {
				require.NoError(t, err)
				require.True(t, wiped)
			}
		})
	}
}

func TestGlobWipeDoesNotRepeatAfterPartialFailure(t *testing.T) {
	path, resolver := globDeviceFixture(t)
	ctx := context.Background()
	vg := &v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "test"}, Spec: v1alpha1.LVMVolumeGroupSpec{DeviceSelector: &v1alpha1.DeviceSelector{
		Paths: []v1alpha1.DevicePath{path}, ForceWipeDevicesAndDestroyAllData: ptr.To(true),
	}}}
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vg.DeepCopy()).Build()
	require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(vg), vg))
	devices := []lsblk.BlockDevice{testWipeDisk("/dev/sda", "8:0"), testWipeDisk("/dev/sdb", "8:16")}
	LSBLK := lsblkmocks.NewMockLSBLK(t)
	LSBLK.EXPECT().ListBlockDevices(ctx).Return(devices, nil).Twice()
	LVM := lvmmocks.NewMockLVM(t)
	LVM.EXPECT().ListPVs(ctx, "").Return([]lvm.PhysicalVolume(nil), nil).Twice()
	wipeClient := wipefsmocks.NewMockWipefs(t)
	wipeClient.EXPECT().Wipe(ctx, "/dev/sda").Return(nil).Once()
	wipeClient.EXPECT().Wipe(ctx, "/dev/sdb").Return(errors.New("device I/O error")).Once()
	r := &Reconciler{Client: k8sClient, NodeName: "node", LSBLK: LSBLK, LVM: LVM, Wipefs: wipeClient}
	wiped, err := r.wipeDevices(ctx, vg, devices, nil, resolver)
	require.True(t, wiped)
	require.ErrorContains(t, err, "device I/O error")
	stored := &v1alpha1.LVMVolumeGroup{}
	require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(vg), stored))
	require.Contains(t, stored.Annotations, constants.DevicesWipedAnnotationPrefix+"node")
	wiped, err = r.wipeDevices(ctx, stored, devices, nil, resolver.Fresh())
	require.NoError(t, err)
	require.False(t, wiped)
}

func TestGlobWipeSkipsMountedAndOwnedMatches(t *testing.T) {
	for _, optional := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "optional"}[optional], func(t *testing.T) {
			path, resolver := globDeviceFixture(t)
			selector := &v1alpha1.DeviceSelector{Paths: []v1alpha1.DevicePath{path}, ForceWipeDevicesAndDestroyAllData: ptr.To(true)}
			if optional {
				selector.OptionalPaths, selector.Paths = selector.Paths, nil
			}
			vg := &v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "test"}, Spec: v1alpha1.LVMVolumeGroupSpec{DeviceSelector: selector}}
			scheme := runtime.NewScheme()
			require.NoError(t, v1alpha1.AddToScheme(scheme))
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vg.DeepCopy()).Build()
			ctx := context.Background()
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(vg), vg))
			devices := []lsblk.BlockDevice{testWipeDisk("/dev/sda", "8:0"), testWipeDisk("/dev/sdb", "8:16"), testWipeDisk("/dev/sdc", "8:32")}
			devices[1].Mountpoints = ptr.To([]*string{ptr.To("/data")})
			pvs := []lvm.PhysicalVolume{{PvName: "/dev/sdc", VgName: "other"}}
			lsblkClient := lsblkmocks.NewMockLSBLK(t)
			lsblkClient.EXPECT().ListBlockDevices(ctx).Return(devices, nil).Once()
			lvmClient := lvmmocks.NewMockLVM(t)
			lvmClient.EXPECT().ListPVs(ctx, "").Return(pvs, nil).Once()
			wipeClient := wipefsmocks.NewMockWipefs(t)
			wipeClient.EXPECT().Wipe(ctx, "/dev/sda").Return(nil).Once()
			r := &Reconciler{Client: k8sClient, NodeName: "node", LSBLK: lsblkClient, LVM: lvmClient, Wipefs: wipeClient}
			wiped, err := r.wipeDevices(ctx, vg, devices, pvs, resolver)
			require.NoError(t, err)
			require.True(t, wiped)
			wipeClient.AssertNotCalled(t, "Wipe", ctx, "/dev/sdb")
			wipeClient.AssertNotCalled(t, "Wipe", ctx, "/dev/sdc")
		})
	}
}

func TestGlobWipeRejectsIncompleteRequiredExpansionBeforeMarker(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	device := filepath.Join(dir, "disk")
	require.NoError(t, os.WriteFile(device, nil, 0600))
	require.NoError(t, os.Symlink(device, filepath.Join(dir, "alias-good")))
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "alias-broken")))
	selector := &v1alpha1.DeviceSelector{Paths: []v1alpha1.DevicePath{v1alpha1.DevicePath("glob:" + filepath.Join(dir, "alias-*"))}, ForceWipeDevicesAndDestroyAllData: ptr.To(true)}
	vg := &v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{Name: "vg1"}, Spec: v1alpha1.LVMVolumeGroupSpec{DeviceSelector: selector}}
	wipeClient := wipefsmocks.NewMockWipefs(t)
	r := &Reconciler{NodeName: "node", Wipefs: wipeClient}
	wiped, err := r.wipeDevices(context.Background(), vg, []lsblk.BlockDevice{testWipeDisk(device, "8:0")}, nil, symlinkResolver.NewWithDefaultResolver())
	require.ErrorContains(t, err, "cannot resolve required device")
	require.False(t, wiped)
	require.Empty(t, vg.Annotations)
}
