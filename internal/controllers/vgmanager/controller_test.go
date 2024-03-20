package vgmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	secv1 "github.com/openshift/api/security/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/filter"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lsblk"
	lsblkmocks "github.com/openshift/lvm-operator/internal/controllers/vgmanager/lsblk/mocks"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvm"
	lvmmocks "github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvm/mocks"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvmd"
	lvmdmocks "github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvmd/mocks"
	wipefsmocks "github.com/openshift/lvm-operator/internal/controllers/vgmanager/wipefs/mocks"
	"github.com/stretchr/testify/mock"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
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
		It("thin provisioned volume group", func(ctx SpecContext) {
			testVGWithLocalDevice(ctx, lvmv1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: lvmv1alpha1.LVMVolumeGroupSpec{
					ThinPoolConfig: &lvmv1alpha1.ThinPoolConfig{
						Name:               "thin-pool-1",
						SizePercent:        90,
						OverprovisionRatio: 10,
					},
				},
			})
		})
		It("thick provisioned volume group", func(ctx SpecContext) {
			testVGWithLocalDevice(ctx, lvmv1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vg1",
				},
				Spec: lvmv1alpha1.LVMVolumeGroupSpec{},
			})
		})
		Context("edge cases during reconciliation", func() {
			Context("failure in LVM or LSBLK", func() {
				It("reconcile failure because of external errors", testReconcileFailure)
			})
			It("should fail on error while fetching LVMVolumeGroup", testErrorOnGetLVMVolumeGroup)
			It("should correctly handle node selector", testNodeSelector)
			It("should handle LVMD edge cases correctly", testLVMD)
			It("should handle thin pool creation correctly", testThinPoolCreation)
			It("should handle thin pool extension cases correctly", testThinPoolExtension)
		})
		Context("event tests", func() {
			It("should correctly emit events", testEvents)
		})
	})
})

func init() {
	if err := lvmv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		panic(err)
	}
	if err := topolvmv1.AddToScheme(scheme.Scheme); err != nil {
		panic(err)
	}
	if err := snapapi.AddToScheme(scheme.Scheme); err != nil {
		panic(err)
	}
	if err := secv1.Install(scheme.Scheme); err != nil {
		panic(err)
	}
	if err := configv1.Install(scheme.Scheme); err != nil {
		panic(err)
	}
}

type testInstances struct {
	LVM    *lvmmocks.MockLVM
	LSBLK  *lsblkmocks.MockLSBLK
	LVMD   lvmd.Configurator
	Wipefs *wipefsmocks.MockWipefs

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
	mockWipefs := wipefsmocks.NewMockWipefs(t)
	testLVMD := lvmd.NewFileConfigurator(filepath.Join(t.TempDir(), "lvmd.yaml"))

	hostname := "test-host.vgmanager.test.io"
	hostnameLabelKey := "kubernetes.io/hostname"

	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "openshift-storage"}}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node", Labels: map[string]string{
		hostnameLabelKey: hostname,
	}}}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(node, namespace).
		Build()
	fakeRecorder := record.NewFakeRecorder(100)
	fakeRecorder.IncludeObject = true

	return testInstances{
		LVM:       mockLVM,
		LSBLK:     mockLSBLK,
		LVMD:      testLVMD,
		Wipefs:    mockWipefs,
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
			Wipefs:        mockWipefs,
			NodeName:      node.GetName(),
			Namespace:     namespace.GetName(),
			Filters:       filter.DefaultFilters,
		},
	}
}

func testVGWithLocalDevice(ctx context.Context, vgTemplate lvmv1alpha1.LVMVolumeGroup) {
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	ctx = log.IntoContext(ctx, logger)

	By("injecting mocked LVM and LSBLK")
	instances := setupInstances()

	var blockDevice lsblk.BlockDevice
	device := getKNameFromDevice(filepath.Join(GinkgoT().TempDir(), "mock0"))
	By("setting up the disk as a mocked block device with losetup", func() {
		// required create to survive valid device check
		_, err := os.Create(device)
		Expect(err).To(Succeed())
		blockDevice = createMockedBlockDevice(device)
	})

	vg := &vgTemplate
	if vg.Spec.DeviceSelector == nil {
		By("defaulting volume group device selector to the temporary device", func() {
			vg.Spec.DeviceSelector = &lvmv1alpha1.DeviceSelector{Paths: []string{device}}
		})
	}
	if vg.GetNamespace() == "" {
		By("defaulting the volume group namespace to the test instance", func() {
			vg.SetNamespace(instances.namespace.GetName())
		})
	}
	if vg.Spec.NodeSelector == nil {
		By("defaulting the volume group node selector to the test instance", func() {
			vg.Spec.NodeSelector = instances.nodeSelector.DeepCopy()
		})
	}

	By("creating the LVMVolumeGroup with the temporary device", func() {
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
		instances.LVM.EXPECT().ListVGs(ctx, true).Return(nil, nil).Once()
		instances.LVM.EXPECT().ListPVs(ctx, "").Return(nil, nil).Twice()
		instances.LSBLK.EXPECT().ListBlockDevices(ctx).Return([]lsblk.BlockDevice{blockDevice}, nil).Once()
		instances.LSBLK.EXPECT().BlockDeviceInfos(ctx, mock.Anything).Return(lsblk.BlockDeviceInfos{
			blockDevice.KName: {
				IsUsableLoopDev: false,
			},
		}, nil).Times(3)
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
	instances.LVM.EXPECT().ListVGs(ctx, true).Return(nil, nil).Once()
	instances.LSBLK.EXPECT().ListBlockDevices(ctx).Return([]lsblk.BlockDevice{blockDevice}, nil).Once()
	instances.LVM.EXPECT().ListPVs(ctx, "").Return(nil, nil).Once()

	// addDevicesToVG
	var lvmPV lvm.PhysicalVolume
	var lvmVG lvm.VolumeGroup
	By("mocking the adding of the device to the volume group", func() {
		lvmPV = lvm.PhysicalVolume{PvName: device}
		lvmVG = lvm.VolumeGroup{
			Name: vg.GetName(),
			PVs:  []lvm.PhysicalVolume{lvmPV},
		}
		instances.LVM.EXPECT().CreateVG(ctx, lvmVG).Return(nil).Once()
	})

	// addThinPoolToVG
	var createdVG lvm.VolumeGroup
	var thinPool lvm.LogicalVolume

	if vg.Spec.ThinPoolConfig != nil {
		By("mocking the creation of the thin pool in the vg", func() {
			instances.LVM.EXPECT().ListLVs(ctx, lvmVG.Name).Return(&lvm.LVReport{Report: make([]lvm.LVReportItem, 0)}, nil).Once()
			instances.LVM.EXPECT().CreateLV(ctx, vg.Spec.ThinPoolConfig.Name, vg.GetName(), vg.Spec.ThinPoolConfig.SizePercent).Return(nil).Once()
		})
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
			instances.LVM.EXPECT().ListLVs(ctx, vg.GetName()).Return(&lvm.LVReport{Report: []lvm.LVReportItem{{
				Lv: []lvm.LogicalVolume{thinPool},
			}}}, nil).Once()
			instances.LVM.EXPECT().ActivateLV(ctx, thinPool.Name, vg.GetName()).Return(nil).Once()
		})
	} else {
		By("ignoring the thin pool creation as it is not present in the VG spec")
		createdVG = lvm.VolumeGroup{
			Name:   vg.GetName(),
			VgSize: "1.0G",
			PVs:    []lvm.PhysicalVolume{lvmPV},
		}
	}
	instances.LVM.EXPECT().ListVGs(ctx, true).Return([]lvm.VolumeGroup{createdVG}, nil).Once()

	By("triggering the next reconciliation after the creation of the thin pool", func() {
		_, err := instances.Reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
		Expect(err).ToNot(HaveOccurred())
	})

	By("verifying the lvmd config generation", func() {
		checkDistributedEvent(corev1.EventTypeNormal, "lvmd config file doesn't exist, will attempt to create a fresh config")
		checkDistributedEvent(corev1.EventTypeNormal, "updated lvmd config with new deviceClasses")
		lvmdConfig, err := instances.LVMD.Load(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(lvmdConfig).ToNot(BeNil())
		Expect(lvmdConfig.DeviceClasses).ToNot(BeNil())
		Expect(lvmdConfig.DeviceClasses).To(HaveLen(1))
		if vg.Spec.ThinPoolConfig != nil {
			Expect(lvmdConfig.DeviceClasses).To(ContainElement(&lvmd.DeviceClass{
				Name:        vg.GetName(),
				VolumeGroup: vg.GetName(),
				Type:        lvmd.TypeThin,
				ThinPoolConfig: &lvmd.ThinPoolConfig{
					Name:               vg.Spec.ThinPoolConfig.Name,
					OverprovisionRatio: float64(vg.Spec.ThinPoolConfig.OverprovisionRatio),
				},
			}))
		} else {
			Expect(lvmdConfig.DeviceClasses).To(ContainElement(&lvmd.DeviceClass{
				Name:        vg.GetName(),
				VolumeGroup: vg.GetName(),
				Type:        lvmd.TypeThick,
				SpareGB:     ptr.To(uint64(0)),
			}))
		}
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
		if vg.Spec.ThinPoolConfig != nil {
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
		}
		instances.LSBLK.EXPECT().ListBlockDevices(ctx).Return([]lsblk.BlockDevice{blockDevice}, nil).Once()
	})

	By("mocking the now created vg in the next fetch", func() {
		instances.LVM.EXPECT().ListVGs(ctx, true).Return([]lvm.VolumeGroup{createdVG}, nil).Once()
	})

	if vg.Spec.ThinPoolConfig != nil {
		By("mocking the lv activation and verification of the created thin pool", func() {
			report := &lvm.LVReport{Report: []lvm.LVReportItem{{
				Lv: []lvm.LogicalVolume{thinPool},
			}}}
			instances.LVM.EXPECT().ActivateLV(ctx, thinPool.Name, createdVG.Name).Return(nil).Once()
			instances.LVM.EXPECT().ListLVs(ctx, vg.GetName()).Return(report, nil).Once()
		})
	}

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

	By("triggering the delete of the VolumeGroup", func() {
		Expect(instances.client.Delete(ctx, vg)).To(Succeed())
		Expect(instances.client.Get(ctx, client.ObjectKeyFromObject(vg), vg)).To(Succeed())
		Expect(vg.Finalizers).ToNot(BeEmpty())
		Expect(vg.DeletionTimestamp).ToNot(BeNil())
		instances.LVM.EXPECT().ListVGs(ctx, true).Return([]lvm.VolumeGroup{{Name: createdVG.Name}}, nil).Once()

		if vg.Spec.ThinPoolConfig != nil {
			instances.LVM.EXPECT().LVExists(ctx, thinPool.Name, createdVG.Name).Return(true, nil).Once()
			instances.LVM.EXPECT().DeleteLV(ctx, thinPool.Name, createdVG.Name).Return(nil).Once()
		}

		instances.LVM.EXPECT().DeleteVG(ctx, lvm.VolumeGroup{Name: createdVG.Name}).Return(nil).Once()
		_, err := instances.Reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
		Expect(err).ToNot(HaveOccurred())
		Expect(instances.client.Get(ctx, client.ObjectKeyFromObject(vg), vg)).ToNot(Succeed())
		lvmdConfig, err := instances.LVMD.Load(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(lvmdConfig).To(Not(BeNil()), "cached lvmd config is still present")
	})
}

func createMockedBlockDevice(device string) lsblk.BlockDevice {
	return lsblk.BlockDevice{
		Name:       "mock0",
		KName:      device,
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
}

func testNodeSelector(ctx context.Context) {
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	ctx = log.IntoContext(ctx, logger)
	volumeGroup := &lvmv1alpha1.LVMVolumeGroup{}
	volumeGroup.SetName("vg1")
	volumeGroup.SetNamespace("openshift-storage")
	volumeGroup.Spec.NodeSelector = &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
		MatchExpressions: []corev1.NodeSelectorRequirement{{
			Key:      "kubernetes.io/hostname",
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{"test-node"},
		}},
	}}}

	invalidVolumeGroup := volumeGroup.DeepCopy()
	invalidVolumeGroup.SetName("vg2")
	invalidVolumeGroup.Spec.NodeSelector = &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
		MatchExpressions: []corev1.NodeSelectorRequirement{{
			Key:      "kubernetes.io/hostname",
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{"test-node-not-existing"},
		}},
	}}}

	matchingNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node", Labels: map[string]string{
		"kubernetes.io/hostname": "test-node",
	}}}
	notMatchingNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node-2", Labels: map[string]string{
		"kubernetes.io/hostname": "test-node-2",
	}}}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(matchingNode, notMatchingNode, volumeGroup, invalidVolumeGroup).
		Build()
	r := &Reconciler{Client: fakeClient, Scheme: scheme.Scheme, NodeName: "test-node"}
	By("first verifying correct node resolution")
	res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(volumeGroup)})
	Expect(err).ToNot(HaveOccurred(), "should not error on valid node selector")
	Expect(res).To(Equal(reconcile.Result{}))

	By("then verifying correct node resolution with invalid node selector (skipping reconcile)")
	res, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(invalidVolumeGroup)})
	Expect(err).ToNot(HaveOccurred(), "should not error on invalid node selector, but filter out")
	Expect(res).To(Equal(reconcile.Result{}))

	By("then verifying incorrect node resolution because nodestatus cannot be created")
	funcs := interceptor.Funcs{Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
		if obj.GetName() == "test-node" {
			return fmt.Errorf("mock creation failure for LVMVolumeGroupNodeStatus")
		}
		return client.Create(ctx, obj, opts...)
	}}
	fakeClient = fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(matchingNode, notMatchingNode, volumeGroup, invalidVolumeGroup).
		WithInterceptorFuncs(funcs).
		Build()
	r = &Reconciler{Client: fakeClient, Scheme: scheme.Scheme, NodeName: "test-node"}
	By("verifying incorrect node resolution because nodestatus cannot be created")
	res, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(volumeGroup)})
	Expect(err).To(HaveOccurred(), "should error on valid node selector due to failure of nodestatus creation")
	Expect(res).To(Equal(reconcile.Result{}))

	fakeClient = fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(matchingNode, notMatchingNode, volumeGroup, invalidVolumeGroup).
		Build()
	r = &Reconciler{Client: fakeClient, Scheme: scheme.Scheme}
	By("Verifying node match error if NodeName is not set")
	res, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(volumeGroup)})
	Expect(err).To(HaveOccurred(), "should error during node match resolution")
	Expect(res).To(Equal(reconcile.Result{}))

	By("then verifying incorrect node resolution because nodestatus cannot be created")
	funcs = interceptor.Funcs{Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		if key.Name == matchingNode.Name {
			if nodeStatus, ok := obj.(*lvmv1alpha1.LVMVolumeGroupNodeStatus); ok {
				return fmt.Errorf("mock get failure for LVMVolumeGroupNodeStatus %s", nodeStatus.GetName())
			}
		}
		return client.Get(ctx, key, obj, opts...)
	}}
	fakeClient = fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(matchingNode, notMatchingNode, volumeGroup, invalidVolumeGroup).
		WithInterceptorFuncs(funcs).
		Build()
	r = &Reconciler{Client: fakeClient, Scheme: scheme.Scheme, NodeName: "test-node"}
	By("verifying incorrect node resolution because nodestatus cannot be fetched from cluster")
	res, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(volumeGroup)})
	Expect(err).To(HaveOccurred(), "should error on valid node selector due to failure of nodestatus fetch")
	Expect(res).To(Equal(reconcile.Result{}))
}

func testErrorOnGetLVMVolumeGroup(ctx context.Context) {
	funcs := interceptor.Funcs{Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		return fmt.Errorf("mock get failure for LVMVolumeGroup %s", key.Name)
	}}
	r := &Reconciler{Client: fake.NewClientBuilder().
		WithInterceptorFuncs(funcs).
		WithScheme(scheme.Scheme).Build(), Scheme: scheme.Scheme}
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&lvmv1alpha1.LVMVolumeGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "openshift-storage"},
	})})
	Expect(err).To(HaveOccurred(), "should error if volume group cannot be fetched")
}

func testEvents(ctx context.Context) {
	fakeRecorder := record.NewFakeRecorder(3)
	fakeRecorder.IncludeObject = true

	vg := &lvmv1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"}}
	vg.SetOwnerReferences([]metav1.OwnerReference{{Name: "owner", Kind: "Owner", UID: "123", APIVersion: "v1alpha1"}})
	nodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}}
	gvk, _ := apiutil.GVKForObject(nodeStatus, scheme.Scheme)
	nodeStatus.SetGroupVersionKind(gvk)

	clnt := fake.NewClientBuilder().WithObjects(vg, nodeStatus).WithScheme(scheme.Scheme).Build()
	r := &Reconciler{Client: clnt, Scheme: scheme.Scheme, EventRecorder: fakeRecorder, NodeName: nodeStatus.GetName()}

	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	ctx = log.IntoContext(ctx, logger)

	r.NormalEvent(ctx, vg, "normal_reason", "message")
	Eventually(ctx, fakeRecorder.Events).Should(Receive(
		Equal("Normal normal_reason message involvedObject{kind=LVMVolumeGroupNodeStatus,apiVersion=lvm.topolvm.io/v1alpha1}")))
	Eventually(ctx, fakeRecorder.Events).Should(Receive(
		Equal("Normal normal_reason update on node /test-node in volume group test/test: message involvedObject{kind=Owner,apiVersion=v1alpha1}")))
	Eventually(ctx, fakeRecorder.Events).Should(Receive(
		Equal("Normal normal_reason update on node /test-node: message involvedObject{kind=,apiVersion=}")))

	r.WarningEvent(ctx, vg, "warning_reason", errors.New("test"))
	Eventually(ctx, fakeRecorder.Events).Should(Receive(
		Equal("Warning warning_reason test involvedObject{kind=LVMVolumeGroupNodeStatus,apiVersion=lvm.topolvm.io/v1alpha1}")))
	Eventually(ctx, fakeRecorder.Events).Should(Receive(
		Equal("Warning warning_reason error on node /test-node in volume group test/test: test involvedObject{kind=Owner,apiVersion=v1alpha1}")))
	Eventually(ctx, fakeRecorder.Events).Should(Receive(
		Equal("Warning warning_reason error on node /test-node: test involvedObject{kind=,apiVersion=}")))
}

func testLVMD(ctx context.Context) {
	r := &Reconciler{Scheme: scheme.Scheme, NodeName: "test", Namespace: "test"}
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	ctx = log.IntoContext(ctx, logger)

	vg := &lvmv1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"}}
	devices := FilteredBlockDevices{}

	r.Client = fake.NewClientBuilder().WithObjects(vg).WithScheme(scheme.Scheme).Build()
	mockLVMD := lvmdmocks.NewMockConfigurator(GinkgoT())
	r.LVMD = mockLVMD
	mockLVM := lvmmocks.NewMockLVM(GinkgoT())
	r.LVM = mockLVM

	mockLVMD.EXPECT().Load(ctx).Once().Return(nil, fmt.Errorf("mock load failure"))
	err := r.applyLVMDConfig(ctx, vg, []lvm.VolumeGroup{}, devices)
	Expect(err).To(HaveOccurred(), "should error if lvmd config cannot be loaded")

	mockLVMD.EXPECT().Load(ctx).Once().Return(&lvmd.Config{}, nil)
	mockLVMD.EXPECT().Save(ctx, mock.Anything).Once().Return(fmt.Errorf("mock save failure"))
	err = r.applyLVMDConfig(ctx, vg, []lvm.VolumeGroup{}, devices)
	Expect(err).To(HaveOccurred(), "should error if lvmd config cannot be saved")
}

func testThinPoolExtension(ctx context.Context) {
	r := &Reconciler{Scheme: scheme.Scheme}
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	ctx = log.IntoContext(ctx, logger)
	mockLVM := lvmmocks.NewMockLVM(GinkgoT())
	r.LVM = mockLVM

	err := r.extendThinPool(ctx, "vg1", "", &lvmv1alpha1.ThinPoolConfig{})
	Expect(err).To(HaveOccurred(), "should error if lvSize is empty")

	err = r.extendThinPool(ctx, "vg1", "1", &lvmv1alpha1.ThinPoolConfig{})
	Expect(err).To(HaveOccurred(), "should error if lvSize has no unit")

	mockLVM.EXPECT().GetVG(ctx, "vg1").Once().Return(lvm.VolumeGroup{}, fmt.Errorf("mocked error"))
	err = r.extendThinPool(ctx, "vg1", "26.96g", &lvmv1alpha1.ThinPoolConfig{})
	Expect(err).To(HaveOccurred(), "should error if GetVG fails")

	err = r.extendThinPool(ctx, "vg1", "26.96gxxx", &lvmv1alpha1.ThinPoolConfig{})
	Expect(err).To(HaveOccurred(), "should error if lvSize is malformatted")

	lvmVG := lvm.VolumeGroup{Name: "vg1"}
	mockLVM.EXPECT().GetVG(ctx, "vg1").Return(lvmVG, nil).Once()
	err = r.extendThinPool(ctx, "vg1", "2g", &lvmv1alpha1.ThinPoolConfig{})
	Expect(err).To(HaveOccurred(), "should error if vgSize is empty")

	lvmVG.VgSize = "1"
	mockLVM.EXPECT().GetVG(ctx, "vg1").Return(lvmVG, nil).Once()
	err = r.extendThinPool(ctx, "vg1", "2g", &lvmv1alpha1.ThinPoolConfig{})
	Expect(err).To(HaveOccurred(), "should error if vgSize has no unit")

	lvmVG.VgSize = "1m"
	mockLVM.EXPECT().GetVG(ctx, "vg1").Return(lvmVG, nil).Once()
	err = r.extendThinPool(ctx, "vg1", "2g", &lvmv1alpha1.ThinPoolConfig{})
	Expect(err).To(HaveOccurred(), "should error if vg unit does not match lv unit")

	lvmVG.VgSize = "1m"
	mockLVM.EXPECT().GetVG(ctx, "vg1").Return(lvmVG, nil).Once()
	err = r.extendThinPool(ctx, "vg1", "2m", &lvmv1alpha1.ThinPoolConfig{})
	Expect(err).To(HaveOccurred(), "should error if unit is not gibibytes")

	lvmVG.VgSize = "1123xxg"
	mockLVM.EXPECT().GetVG(ctx, "vg1").Return(lvmVG, nil).Once()
	err = r.extendThinPool(ctx, "vg1", "2g", &lvmv1alpha1.ThinPoolConfig{})
	Expect(err).To(HaveOccurred(), "should error if vgSize is malformatted")

	lvmVG.VgSize = "3g"
	mockLVM.EXPECT().GetVG(ctx, "vg1").Return(lvmVG, nil).Once()
	err = r.extendThinPool(ctx, "vg1", "3g", &lvmv1alpha1.ThinPoolConfig{})
	Expect(err).ToNot(HaveOccurred(), "should fast skip if no expansion is needed")

	lvmVG.VgSize = "5g"
	thinPool := &lvmv1alpha1.ThinPoolConfig{Name: "thin-pool-1", SizePercent: 90}
	mockLVM.EXPECT().GetVG(ctx, "vg1").Return(lvmVG, nil).Once()
	mockLVM.EXPECT().ExtendLV(ctx, thinPool.Name, "vg1", thinPool.SizePercent).
		Once().Return(fmt.Errorf("failed to extend lv"))
	err = r.extendThinPool(ctx, "vg1", "3g", thinPool)
	Expect(err).To(HaveOccurred(), "should fail if lvm extension fails")

	mockLVM.EXPECT().GetVG(ctx, "vg1").Return(lvmVG, nil).Once()
	mockLVM.EXPECT().ExtendLV(ctx, thinPool.Name, "vg1", thinPool.SizePercent).
		Once().Return(nil)
	err = r.extendThinPool(ctx, "vg1", "3g", thinPool)
	Expect(err).ToNot(HaveOccurred(), "succeed if lvm extension succeeds")
}

func testThinPoolCreation(ctx context.Context) {
	r := &Reconciler{Scheme: scheme.Scheme}
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	ctx = log.IntoContext(ctx, logger)
	mockLVM := lvmmocks.NewMockLVM(GinkgoT())
	r.LVM = mockLVM

	err := r.addThinPoolToVG(ctx, "vg1", nil)
	Expect(err).To(HaveOccurred(), "should error if thin pool config is nil")

	mockLVM.EXPECT().ListLVs(ctx, "vg1").Once().Return(nil, fmt.Errorf("report error"))
	err = r.addThinPoolToVG(ctx, "vg1", &lvmv1alpha1.ThinPoolConfig{})
	Expect(err).To(HaveOccurred(), "should error if list lvs report fails")

	mockLVM.EXPECT().ListLVs(ctx, "vg1").Once().Return(&lvm.LVReport{Report: []lvm.LVReportItem{{
		Lv: []lvm.LogicalVolume{{Name: "thin-pool-1", VgName: "vg1", LvAttr: "blub"}},
	}}}, nil)
	err = r.addThinPoolToVG(ctx, "vg1", &lvmv1alpha1.ThinPoolConfig{Name: "thin-pool-1"})
	Expect(err).To(HaveOccurred(), "should error if thin pool attributes cannot be parsed")

	mockLVM.EXPECT().ListLVs(ctx, "vg1").Once().Return(&lvm.LVReport{Report: []lvm.LVReportItem{{
		Lv: []lvm.LogicalVolume{{Name: "thin-pool-1", VgName: "vg1", LvAttr: "rwi---tz--"}},
	}}}, nil)
	err = r.addThinPoolToVG(ctx, "vg1", &lvmv1alpha1.ThinPoolConfig{Name: "thin-pool-1"})
	Expect(err).To(HaveOccurred(), "should error if volume that is not thin pool already exists")

	thinPool := &lvmv1alpha1.ThinPoolConfig{Name: "thin-pool-1", SizePercent: 90}

	mockLVM.EXPECT().ListLVs(ctx, "vg1").Once().Return(&lvm.LVReport{Report: []lvm.LVReportItem{{
		Lv: []lvm.LogicalVolume{},
	}}}, nil)
	mockLVM.EXPECT().CreateLV(ctx, thinPool.Name, "vg1", thinPool.SizePercent).Once().Return(fmt.Errorf("mocked error"))
	err = r.addThinPoolToVG(ctx, "vg1", thinPool)
	Expect(err).To(HaveOccurred(), "should create thin pool if it does not exist, but should fail if that does not work")

	mockLVM.EXPECT().ListLVs(ctx, "vg1").Once().Return(&lvm.LVReport{Report: []lvm.LVReportItem{{
		Lv: []lvm.LogicalVolume{},
	}}}, nil)
	mockLVM.EXPECT().CreateLV(ctx, thinPool.Name, "vg1", thinPool.SizePercent).Once().Return(nil)
	err = r.addThinPoolToVG(ctx, "vg1", thinPool)
	Expect(err).ToNot(HaveOccurred(), "should create thin pool if it does not exist")

	lvmVG := lvm.VolumeGroup{Name: "vg1", VgSize: "5g"}
	mockLVM.EXPECT().ListLVs(ctx, "vg1").Once().Return(&lvm.LVReport{Report: []lvm.LVReportItem{{
		Lv: []lvm.LogicalVolume{{Name: "thin-pool-1", VgName: "vg1", LvAttr: "twi---tz--", LvSize: "3g"}},
	}}}, nil)
	mockLVM.EXPECT().GetVG(ctx, "vg1").Once().Return(lvmVG, nil)
	mockLVM.EXPECT().ExtendLV(ctx, thinPool.Name, "vg1", thinPool.SizePercent).
		Once().Return(nil)
	err = r.addThinPoolToVG(ctx, "vg1", thinPool)
	Expect(err).ToNot(HaveOccurred(), "should not error if thin pool already exists, extension should work")
}

func testReconcileFailure(ctx context.Context) {
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	ctx = log.IntoContext(ctx, logger)

	By("injecting mocked LVM and LSBLK")
	instances := setupInstances()
	mockLVMD := lvmdmocks.NewMockConfigurator(GinkgoT())
	instances.LVMD = mockLVMD

	vg := &lvmv1alpha1.LVMVolumeGroup{}
	By("creating the LVMVolumeGroup with the mocked device", func() {
		vg.SetName("vg1")
		vg.SetNamespace(instances.namespace.GetName())
		vg.Spec.NodeSelector = instances.nodeSelector.DeepCopy()
		vg.Spec.DeviceSelector = &lvmv1alpha1.DeviceSelector{Paths: []string{"/dev/sda"}}
		vg.Spec.DeviceSelector.ForceWipeDevicesAndDestroyAllData = ptr.To(true)
		vg.Spec.ThinPoolConfig = &lvmv1alpha1.ThinPoolConfig{
			Name:               "thin-pool-1",
			SizePercent:        90,
			OverprovisionRatio: 10,
		}
		Expect(instances.client.Create(ctx, vg)).To(Succeed())
		// First reconcile adds finalizer
		_, err := instances.Reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
		Expect(err).ToNot(HaveOccurred())
	})

	By("triggering listblockdevices failure", func() {
		instances.LSBLK.EXPECT().ListBlockDevices(ctx).Once().Return(nil, fmt.Errorf("mocked error"))
		_, err := instances.Reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to list block devices"))
	})

	By("triggering wipefs failure", func() {
		evalSymlinks = func(path string) (string, error) {
			return path, nil
		}
		defer func() {
			evalSymlinks = filepath.EvalSymlinks
		}()
		instances.LSBLK.EXPECT().ListBlockDevices(ctx).Once().Return([]lsblk.BlockDevice{
			{Name: "/dev/sda", KName: "/dev/sda", FSType: "ext4"},
		}, nil)
		instances.Wipefs.EXPECT().Wipe(ctx, "/dev/sda").Once().Return(fmt.Errorf("mocked error"))
		_, err := instances.Reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to wipe devices"))
	})

	By("triggering lsblk failure after wipefs", func() {
		evalSymlinks = func(path string) (string, error) {
			return path, nil
		}
		defer func() {
			evalSymlinks = filepath.EvalSymlinks
		}()
		instances.LSBLK.EXPECT().ListBlockDevices(ctx).Once().Return([]lsblk.BlockDevice{
			{Name: "/dev/sda", KName: "/dev/sda", FSType: "ext4"},
		}, nil)
		instances.Wipefs.EXPECT().Wipe(ctx, "/dev/sda").Once().Return(nil)
		instances.LSBLK.EXPECT().ListBlockDevices(ctx).Once().Return(nil, fmt.Errorf("mocked error"))
		_, err := instances.Reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to list block devices after wiping devices"))
	})

	By("triggering failure on fetching new devices to add to volume group", func() {
		evalSymlinks = func(path string) (string, error) {
			return path, nil
		}
		defer func() {
			evalSymlinks = filepath.EvalSymlinks
		}()
		blockDevices := []lsblk.BlockDevice{
			{Name: "/dev/xxx", KName: "/dev/xxx", FSType: "ext4"},
		}
		instances.LSBLK.EXPECT().ListBlockDevices(ctx).Once().Return(blockDevices, nil)
		instances.LVM.EXPECT().ListVGs(ctx, true).Once().Return(nil, nil)
		_, err := instances.Reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
		nodeStatus := &lvmv1alpha1.LVMVolumeGroupNodeStatus{}
		Expect(instances.client.Get(ctx, client.ObjectKey{
			Namespace: instances.namespace.GetName(),
			Name:      instances.node.GetName(),
		}, nodeStatus)).To(Succeed())
		Expect(nodeStatus.Spec.LVMVGStatus).To(HaveLen(1))
		Expect(nodeStatus.Spec.LVMVGStatus[0].Status).To(Equal(lvmv1alpha1.VGStatusFailed))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unable to validate device"))
	})

	Expect(instances.client.Get(ctx, client.ObjectKeyFromObject(vg), vg)).To(Succeed())
	vg.Spec.DeviceSelector.ForceWipeDevicesAndDestroyAllData = ptr.To(false)
	Expect(instances.client.Update(ctx, vg)).To(Succeed())

	By("triggering failure because vg is not found even though there are no devices to be added", func() {
		evalSymlinks = func(path string) (string, error) {
			return path, nil
		}
		defer func() {
			evalSymlinks = filepath.EvalSymlinks
		}()
		instances.LSBLK.EXPECT().ListBlockDevices(ctx).Once().Return([]lsblk.BlockDevice{
			{Name: "/dev/sda", KName: "/dev/sda", FSType: "xfs", PartLabel: "reserved"},
		}, nil)
		instances.LVM.EXPECT().ListPVs(ctx, "").Once().Return(nil, nil)
		instances.LSBLK.EXPECT().BlockDeviceInfos(ctx, mock.Anything).Once().Return(lsblk.BlockDeviceInfos{
			"/dev/sda": {
				IsUsableLoopDev: false,
			},
		}, nil)
		_, err := instances.Reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("the volume group vg1 does not exist and there were no available devices to create it"))
	})

	By("triggering failure because vg is found but thin-pool validation failed", func() {
		evalSymlinks = func(path string) (string, error) {
			return path, nil
		}
		defer func() {
			evalSymlinks = filepath.EvalSymlinks
		}()
		instances.LSBLK.EXPECT().ListBlockDevices(ctx).Once().Return([]lsblk.BlockDevice{
			{Name: "/dev/sda", KName: "/dev/sda", FSType: "xfs", PartLabel: "reserved"},
		}, nil)
		vgs := []lvm.VolumeGroup{
			{Name: "vg1", VgSize: "1g"},
		}
		instances.LVM.EXPECT().ListVGs(ctx, true).Once().Return(vgs, nil)
		instances.LVM.EXPECT().ListPVs(ctx, "").Once().Return(nil, nil)
		instances.LSBLK.EXPECT().BlockDeviceInfos(ctx, mock.Anything).Once().Return(lsblk.BlockDeviceInfos{
			"/dev/sda": {
				IsUsableLoopDev: false,
			},
		}, nil)
		expectedError := fmt.Errorf("mocked error")
		instances.LVM.EXPECT().ListLVs(ctx, "vg1").Once().Return(nil, expectedError)
		_, err := instances.Reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vg)})
		Expect(err).To(MatchError(expectedError))
	})
}
