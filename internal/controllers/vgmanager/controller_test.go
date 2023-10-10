package vgmanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	secv1 "github.com/openshift/api/security/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/internal/controllers/constants"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lsblk"
	lsblkmocks "github.com/openshift/lvm-operator/internal/controllers/vgmanager/lsblk/mocks"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvm"
	lvmmocks "github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvm/mocks"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvmd"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestVGManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = Describe("vgmanager controller", func() {
	Context("verifying standard behavior with node selector", func() {
		It("should be reconciled successfully with a mocked block device", testMockedBlockDeviceOnHost)
	})
})

type testInstances struct {
	LVM   *lvmmocks.MockLVM
	LSBLK *lsblkmocks.MockLSBLK
	LVMD  lvmd.Configurator

	host      string
	namespace *corev1.Namespace
	node      *corev1.Node

	nodeSelector corev1.NodeSelector
	client       client.WithWatch
	recorder     *record.FakeRecorder

	Reconciler *Reconciler
}

func setupInstances() testInstances {
	GinkgoHelper()
	By("setting up Mocks and Test Instances")
	t := GinkgoT()
	t.Helper()

	mockLSBLK := lsblkmocks.NewMockLSBLK(t)
	mockLVM := lvmmocks.NewMockLVM(t)
	testLVMD := lvmd.NewFileConfigurator(filepath.Join(t.TempDir(), "lvmd.yaml"))

	hostname := "test-host.vgmanager.test.io"
	hostnameLabelKey := "kubernetes.io/hostname"

	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "openshift-storage"}}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node", Labels: map[string]string{
		hostnameLabelKey: hostname,
	}}}

	Expect(lvmv1alpha1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(topolvmv1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(snapapi.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(secv1.Install(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(configv1.Install(scheme.Scheme)).NotTo(HaveOccurred())
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(node, namespace).
		Build()
	fakeRecorder := record.NewFakeRecorder(100)
	fakeRecorder.IncludeObject = true

	return testInstances{
		LVM:       mockLVM,
		LSBLK:     mockLSBLK,
		LVMD:      testLVMD,
		namespace: namespace,
		node:      node,
		host:      hostname,
		recorder:  fakeRecorder,
		nodeSelector: corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
			MatchExpressions: []corev1.NodeSelectorRequirement{{
				Key:      hostnameLabelKey,
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{hostname},
			}},
		}}},
		client: fakeClient,
		Reconciler: &Reconciler{
			Client:        fakeClient,
			Scheme:        scheme.Scheme,
			EventRecorder: fakeRecorder,
			LVMD:          testLVMD,
			LVM:           mockLVM,
			LSBLK:         mockLSBLK,
			NodeName:      node.GetName(),
			Namespace:     namespace.GetName(),
			Filters:       filter.DefaultFilters,
		},
	}
}

func testMockedBlockDeviceOnHost(ctx context.Context) {
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	ctx = log.IntoContext(ctx, logger)

	By("injecting mocked LVM and LSBLK")
	instances := setupInstances()

	var blockDevice lsblk.BlockDevice
	device := filepath.Join(GinkgoT().TempDir(), "mock0")
	By("setting up the disk as a block device with losetup", func() {
		// required create to survive valid device check
		_, err := os.Create(device)
		Expect(err).To(Succeed())
		blockDevice = lsblk.BlockDevice{
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
		}
	})

	vg := &lvmv1alpha1.LVMVolumeGroup{}
	By("creating the LVMVolumeGroup with the temporary device", func() {
		vg.SetName("vg1")
		vg.SetNamespace(instances.namespace.GetName())
		vg.Spec.NodeSelector = instances.nodeSelector.DeepCopy()
		vg.Spec.DeviceSelector = &lvmv1alpha1.DeviceSelector{Paths: []string{device}}
		vg.Spec.ThinPoolConfig = &lvmv1alpha1.ThinPoolConfig{
			Name:               "thin-pool-1",
			SizePercent:        90,
			OverprovisionRatio: 10,
		}
		Expect(instances.client.Create(ctx, vg)).To(Succeed())
	})

	By("triggering the Reconciliation after the VG was created", func() {
		_, err := instances.Reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
		Expect(err).ToNot(HaveOccurred())
	})

	By("verifying the finalizers were set", func() {
		updatedVG := &lvmv1alpha1.LVMVolumeGroup{}
		Expect(instances.client.Get(ctx, client.ObjectKeyFromObject(vg), updatedVG)).To(Succeed())
		Expect(updatedVG.GetFinalizers()).ToNot(BeEmpty())
		Expect(updatedVG.GetFinalizers()).To(HaveLen(1))
	})

	nodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{}
	By("verifying the node status was created", func() {
		nodeStatus.SetName(instances.node.GetName())
		nodeStatus.SetNamespace(instances.namespace.GetName())
		Expect(instances.client.Get(ctx, client.ObjectKeyFromObject(nodeStatus), nodeStatus)).To(Succeed())
		Expect(nodeStatus.Spec.LVMVGStatus).To(BeEmpty())
	})

	checkDistributedEvent := func(eventType, msg string) {
		timeout := 100 * time.Millisecond
		GinkgoHelper()
		Eventually(instances.recorder.Events).WithContext(ctx).WithTimeout(timeout).Should(Receive(SatisfyAll(
			ContainSubstring(msg),
			ContainSubstring(eventType)),
			ContainSubstring("LVMVolumeGroupNodeStatus")))
		Eventually(instances.recorder.Events).WithContext(ctx).WithTimeout(timeout).Should(Receive(SatisfyAll(
			ContainSubstring(fmt.Sprintf("update on node %s", client.ObjectKeyFromObject(nodeStatus))),
			ContainSubstring(msg),
			ContainSubstring(eventType)),
			ContainSubstring("LVMVolumeGroup")))
	}

	By("triggering the second reconciliation after the initial setup", func() {
		instances.LVM.EXPECT().ListVGs().Return(nil, nil).Twice()
		instances.LSBLK.EXPECT().ListBlockDevices().Return([]lsblk.BlockDevice{blockDevice}, nil).Once()
		instances.LSBLK.EXPECT().HasBindMounts(blockDevice).Return(false, "", nil).Once()
		_, err := instances.Reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
		Expect(err).ToNot(HaveOccurred())
	})

	By("ensuring the VGStatus was set to progressing after picking up new devices", func() {
		Expect(instances.client.Get(ctx, client.ObjectKeyFromObject(nodeStatus), nodeStatus)).To(Succeed())
		Expect(nodeStatus.Spec.LVMVGStatus).ToNot(BeEmpty())
		Expect(nodeStatus.Spec.LVMVGStatus).To(ContainElement(lvmv1alpha1.VGStatus{
			Name:   vg.GetName(),
			Status: lvmv1alpha1.VGStatusProgressing,
		}))
	})

	// Requeue effects
	instances.LVM.EXPECT().ListVGs().Return(nil, nil).Twice()
	instances.LSBLK.EXPECT().ListBlockDevices().Return([]lsblk.BlockDevice{blockDevice}, nil).Once()
	instances.LSBLK.EXPECT().HasBindMounts(blockDevice).Return(false, "", nil).Once()

	// addDevicesToVG
	var lvmPV lvm.PhysicalVolume
	var lvmVG lvm.VolumeGroup
	By("mocking the adding of the device to the volume group", func() {
		lvmPV = lvm.PhysicalVolume{PvName: device}
		lvmVG = lvm.VolumeGroup{
			Name: vg.GetName(),
			PVs:  []lvm.PhysicalVolume{lvmPV},
		}
		instances.LVM.EXPECT().CreateVG(lvmVG).Return(nil).Once()
	})

	// addThinPoolToVG
	By("mocking the creation of the thin pool in the vg", func() {
		instances.LVM.EXPECT().ListLVs(lvmVG.Name).Return(&lvm.LVReport{Report: make([]lvm.LVReportItem, 0)}, nil).Once()
		instances.LVM.EXPECT().CreateLV(vg.Spec.ThinPoolConfig.Name, vg.GetName(), vg.Spec.ThinPoolConfig.SizePercent).Return(nil).Once()
	})

	var createdVG lvm.VolumeGroup
	var thinPool lvm.LogicalVolume
	By("mocking the report of LVs to now contain the thin pool", func() {
		// validateLVs
		thinPool = lvm.LogicalVolume{
			Name:            vg.Spec.ThinPoolConfig.Name,
			VgName:          vg.GetName(),
			LvAttr:          "twi---tz--",
			LvSize:          "1.0G",
			MetadataPercent: "10.0",
		}
		createdVG = lvm.VolumeGroup{
			Name:   vg.GetName(),
			VgSize: thinPool.LvSize,
			PVs:    []lvm.PhysicalVolume{lvmPV},
		}
		instances.LVM.EXPECT().ListLVs(vg.GetName()).Return(&lvm.LVReport{Report: []lvm.LVReportItem{{
			Lv: []lvm.LogicalVolume{thinPool},
		}}}, nil).Once()
		instances.LVM.EXPECT().ListVGs().Return([]lvm.VolumeGroup{createdVG}, nil).Twice()
		instances.LVM.EXPECT().ActivateLV(thinPool.Name, vg.GetName()).Return(nil).Once()
	})

	By("triggering the next reconciliation after the creation of the thin pool", func() {
		_, err := instances.Reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
		Expect(err).ToNot(HaveOccurred())
	})

	By("verifying the lvmd config generation", func() {
		checkDistributedEvent(corev1.EventTypeNormal, "lvmd config file doesn't exist, will attempt to create a fresh config")
		checkDistributedEvent(corev1.EventTypeNormal, "updated lvmd config with new deviceClasses")
		lvmdConfig, err := instances.LVMD.Load()
		Expect(err).ToNot(HaveOccurred())
		Expect(lvmdConfig).ToNot(BeNil())
		Expect(lvmdConfig.SocketName).To(Equal(constants.DefaultLVMdSocket))
		Expect(lvmdConfig.DeviceClasses).ToNot(BeNil())
		Expect(lvmdConfig.DeviceClasses).To(HaveLen(1))
		Expect(lvmdConfig.DeviceClasses).To(ContainElement(&lvmd.DeviceClass{
			Name:        vg.GetName(),
			VolumeGroup: vg.GetName(),
			Type:        lvmd.TypeThin,
			ThinPoolConfig: &lvmd.ThinPoolConfig{
				Name:               vg.Spec.ThinPoolConfig.Name,
				OverprovisionRatio: float64(vg.Spec.ThinPoolConfig.OverprovisionRatio),
			},
		}))
	})

	var oldReadyGeneration int64
	By("verifying the VGStatus is now ready", func() {
		checkDistributedEvent(corev1.EventTypeNormal, "all the available devices are attached to the volume group")
		Expect(instances.client.Get(ctx, client.ObjectKeyFromObject(nodeStatus), nodeStatus)).To(Succeed())
		Expect(nodeStatus.Spec.LVMVGStatus).ToNot(BeEmpty())
		Expect(nodeStatus.Spec.LVMVGStatus).To(ContainElement(lvmv1alpha1.VGStatus{
			Name:    vg.GetName(),
			Status:  lvmv1alpha1.VGStatusReady,
			Devices: []string{device},
		}))
		oldReadyGeneration = nodeStatus.GetGeneration()
	})

	By("mocking the now created children in the block device", func() {
		blockDevice.FSType = filter.FSTypeLVM2Member
		blockDevice.Children = []lsblk.BlockDevice{
			{
				Name:   fmt.Sprintf("/dev/mapper/%s-%s_tdata", lvmVG.Name, strings.Replace(vg.Spec.ThinPoolConfig.Name, "-", "--", 2)),
				KName:  "/dev/dm-1",
				FSType: "lvm",
				Children: []lsblk.BlockDevice{{
					Name:  fmt.Sprintf("/dev/mapper/%s-%s", lvmVG.Name, strings.Replace(vg.Spec.ThinPoolConfig.Name, "-", "--", 2)),
					KName: "/dev/dm-2",
				}},
			},
			{
				Name:   fmt.Sprintf("/dev/mapper/%s-%s_tmeta", lvmVG.Name, strings.Replace(vg.Spec.ThinPoolConfig.Name, "-", "--", 2)),
				KName:  "/dev/dm-0",
				FSType: "lvm",
				Children: []lsblk.BlockDevice{{
					Name:  fmt.Sprintf("/dev/mapper/%s-%s", lvmVG.Name, strings.Replace(vg.Spec.ThinPoolConfig.Name, "-", "--", 2)),
					KName: "/dev/dm-2",
				}},
			},
		}
		instances.LSBLK.EXPECT().ListBlockDevices().Return([]lsblk.BlockDevice{blockDevice}, nil).Once()
	})

	By("mocking the now created vg and thin pool", func() {
		instances.LVM.EXPECT().ListVGs().Return([]lvm.VolumeGroup{createdVG}, nil).Once()
		instances.LVM.EXPECT().ListLVs(vg.GetName()).Return(&lvm.LVReport{Report: []lvm.LVReportItem{{
			Lv: []lvm.LogicalVolume{thinPool},
		}}}, nil).Once()
		instances.LVM.EXPECT().ActivateLV(thinPool.Name, createdVG.Name).Return(nil).Once()
	})

	By("triggering the verification reconcile that should confirm the ready state", func() {
		_, err := instances.Reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
		Expect(err).ToNot(HaveOccurred())
	})

	By("verifying the state did not change", func() {
		Expect(instances.client.Get(ctx, client.ObjectKeyFromObject(nodeStatus), nodeStatus)).To(Succeed())
		Expect(nodeStatus.Spec.LVMVGStatus).ToNot(BeEmpty())
		Expect(nodeStatus.Spec.LVMVGStatus).To(ContainElement(lvmv1alpha1.VGStatus{
			Name:    vg.GetName(),
			Status:  lvmv1alpha1.VGStatusReady,
			Devices: []string{device},
		}))
		Expect(oldReadyGeneration).To(Equal(nodeStatus.GetGeneration()))
	})
}
