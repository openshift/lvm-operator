package vgmanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/pkg/lsblk"
	"github.com/openshift/lvm-operator/pkg/lvm"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("vgmanager controller", func() {
	SetDefaultEventuallyTimeout(timeout)
	SetDefaultEventuallyPollingInterval(interval)
	Context("verifying standard behavior with node selector", func() {
		It("should be reconciled successfully with a mocked block device", testMockedBlockDeviceOnHost)
	})
})

func testMockedBlockDeviceOnHost(ctx context.Context) {
	By("injecting mocked LVM and LSBLK")
	mockLVM, mockLSBLK := setupMocks()

	By("setting up the disk as a block device with losetup")
	device := filepath.Join(GinkgoT().TempDir(), "mock0")
	// required create to survive valid device check
	_, err := os.Create(device)
	Expect(err).To(Succeed())

	By("setting up the LVMVolumeGroup with the temporary device")
	vg := &lvmv1alpha1.LVMVolumeGroup{}
	vg.SetName("vg1")
	vg.SetNamespace(testNamespaceName)
	vg.Spec.NodeSelector = testNodeSelector.DeepCopy()
	vg.Spec.DeviceSelector = &lvmv1alpha1.DeviceSelector{Paths: []string{device}}
	vg.Spec.ThinPoolConfig = &lvmv1alpha1.ThinPoolConfig{
		Name:               "thin-pool",
		SizePercent:        90,
		OverprovisionRatio: 10,
	}

	mockLVM.EXPECT().ListVGs().Return(nil, nil).Once()
	mockLSBLK.EXPECT().ListBlockDevices().Return([]lsblk.BlockDevice{
		{
			Name:       "mock0",
			KName:      getKNameFromDevice(device),
			Type:       "mocked",
			Model:      "mocked",
			Vendor:     "mocked",
			State:      "live",
			FSType:     "",
			Size:       "1G",
			Children:   nil,
			Serial:     "MOCK",
			DevicePath: device,
		},
	}, nil).Once()
	// hasBindMounts in filters needs a mock
	mockLSBLK.EXPECT().HasBindMounts(mock.AnythingOfType("BlockDevice")).Return(false, "", nil).Once()

	// Create VG
	lvmPV := lvm.PhysicalVolume{PvName: device}
	lvmVG := lvm.VolumeGroup{
		Name: vg.GetName(),
		PVs:  []lvm.PhysicalVolume{lvmPV},
	}
	mockLVM.EXPECT().CreateVG(lvmVG).Return(nil).Once()

	// Check for Thin Pool
	mockLVM.EXPECT().ListLVs(vg.GetName()).Return(&lvm.LVReport{Report: make([]lvm.LVReportItem, 0)}, nil).Once()

	// Create Thin Pool
	mockLVM.EXPECT().CreateLV(vg.Spec.ThinPoolConfig.Name, vg.GetName(), vg.Spec.ThinPoolConfig.SizePercent).Return(nil).Once()

	// Validate created Thin Pool
	thinPool := lvm.LogicalVolume{
		Name:            vg.Spec.ThinPoolConfig.Name,
		VgName:          vg.GetName(),
		LvAttr:          "twi-a-tz--",
		LvSize:          "1.0G",
		MetadataPercent: "10.0",
	}
	createdVG := lvm.VolumeGroup{
		Name:   vg.GetName(),
		VgSize: thinPool.LvSize,
		PVs:    []lvm.PhysicalVolume{lvmPV},
	}
	mockLVM.EXPECT().ListLVs(vg.GetName()).Return(&lvm.LVReport{Report: []lvm.LVReportItem{{
		Lv: []lvm.LogicalVolume{thinPool},
	}}}, nil)
	// status update needs to access the vgs for node status
	mockLVM.EXPECT().GetVG(vg.GetName()).Return(createdVG, nil).Once()
	mockLVM.EXPECT().ListVGs().Return([]lvm.VolumeGroup{createdVG}, nil).Once()

	Expect(k8sClient.Create(ctx, vg)).To(Succeed())
	DeferCleanup(func(ctx context.Context) {
		By("deleting the LVMVolumeGroup after successful verification")
		mockLVM.EXPECT().LVExists(vg.Spec.ThinPoolConfig.Name, vg.GetName()).Return(true, nil).Once()
		mockLVM.EXPECT().DeleteLV(vg.Spec.ThinPoolConfig.Name, vg.GetName()).Return(nil).Once()
		mockLVM.EXPECT().DeleteVG(createdVG).Return(nil)
		Expect(k8sClient.Delete(ctx, vg)).To(Succeed())
		Eventually(func(ctx context.Context) error {
			return k8sClient.Get(ctx, client.ObjectKeyFromObject(vg), vg)
		}).WithContext(ctx).Should(Satisfy(errors.IsNotFound), "no finalizers should have blocked"+
			"the LVMVolumeGroup deletion and it should not exist on the cluster anymore")
	})

	By("verifying finalizer")
	Eventually(func(g Gomega, ctx context.Context) []string {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(vg), vg)).To(Succeed())
		return vg.GetFinalizers()
	}).WithContext(ctx).Should(ContainElement(fmt.Sprintf("%s/%s", NodeCleanupFinalizer, testNodeName)))

	By("verifying the Node Status contains the Volume Group in Ready State")
	nodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{}
	nodeStatus.SetName(testNodeName)
	nodeStatus.SetNamespace(testNamespaceName)
	Eventually(func(g Gomega, ctx context.Context) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(nodeStatus), nodeStatus)).To(Succeed())
		g.Expect(nodeStatus.Spec.LVMVGStatus).ToNot(BeEmpty(), "volume group needs to be present")
		g.Expect(nodeStatus.Spec.LVMVGStatus).To(ContainElement(lvmv1alpha1.VGStatus{
			Name:    vg.GetName(),
			Status:  lvmv1alpha1.VGStatusReady,
			Devices: vg.Spec.DeviceSelector.Paths,
		}), "volume group needs to be ready and contain all the devices from the selector")
	}).WithContext(ctx).Should(Succeed())
}
