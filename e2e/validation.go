package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	. "github.com/onsi/gomega"
	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	timeout                   = time.Minute * 15
	interval                  = time.Second * 10
	lvmVolumeGroupName        = "vg1"
	storageClassName          = "odf-lvm-vg1"
	volumeSnapshotClassName   = "odf-lvm-vg1"
	csiDriverName             = "topolvm.cybozu.com"
	topolvmNodeDaemonSetName  = "topolvm-node"
	topolvmCtrlDeploymentName = "topolvm-controller"
	vgManagerDaemonsetName    = "vg-manager"
)

// function to validate LVMVolume group.
func validateLVMvg(ctx context.Context) error {
	lvmVG := lvmv1alpha1.LVMVolumeGroup{}

	Eventually(func() bool {
		err := crClient.Get(ctx, types.NamespacedName{Name: lvmVolumeGroupName, Namespace: installNamespace}, &lvmVG)
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("VG found\n")
	return nil
}

// function to validate storage class.
func validateStorageClass(ctx context.Context) error {
	sc := storagev1.StorageClass{}

	Eventually(func() bool {
		err := crClient.Get(ctx, types.NamespacedName{Name: storageClassName, Namespace: installNamespace}, &sc)
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("SC found\n")
	return nil
}

// function to validate volume snapshot class.
func validateVolumeSnapshotClass(ctx context.Context) error {
	vsc := snapapi.VolumeSnapshotClass{}

	Eventually(func() bool {
		err := crClient.Get(ctx, types.NamespacedName{Name: volumeSnapshotClassName}, &vsc)
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("VolumeSnapshotClass found\n")
	return nil
}

// function to validate CSI Driver.
func validateCSIDriver(ctx context.Context) error {
	cd := storagev1.CSIDriver{}

	Eventually(func() bool {
		err := crClient.Get(ctx, types.NamespacedName{Name: csiDriverName, Namespace: installNamespace}, &cd)
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("CSI Driver found\n")
	return nil
}

// function to validate TopoLVM node.
func validateTopolvmNode(ctx context.Context) error {
	ds := appsv1.DaemonSet{}
	Eventually(func() bool {
		err := crClient.Get(ctx, types.NamespacedName{Name: topolvmNodeDaemonSetName, Namespace: installNamespace}, &ds)
		return err == nil
	}, timeout, interval).Should(BeTrue())
	debug("TopoLVM node found\n")

	// checking for the ready status
	Eventually(func() bool {
		err := crClient.Get(ctx, types.NamespacedName{Name: topolvmNodeDaemonSetName, Namespace: installNamespace}, &ds)
		if err != nil {
			debug("topolvmNode : %s", err.Error())
		}
		return ds.Status.DesiredNumberScheduled == ds.Status.NumberReady
	}, timeout, interval).Should(BeTrue())
	debug("TopolvmNode Status is ready\n")

	return nil
}

// function to validate vg manager resource.
func validateVGManager(ctx context.Context) error {
	ds := appsv1.DaemonSet{}

	Eventually(func() bool {
		err := crClient.Get(ctx, types.NamespacedName{Name: vgManagerDaemonsetName, Namespace: installNamespace}, &ds)
		return err == nil
	}, timeout, interval).Should(BeTrue())
	debug("VG manager found\n")

	return nil
}

// function to validate TopoLVM Deployment.
func validateTopolvmController(ctx context.Context) error {
	dep := appsv1.Deployment{}

	Eventually(func() bool {
		err := crClient.Get(ctx, types.NamespacedName{Name: topolvmCtrlDeploymentName, Namespace: installNamespace}, &dep)
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("topolvm-controller deployment found\n")
	return nil
}

// Validate all the resources created by LVMO.
func validateResources() {

	var ctx = context.Background()
	Describe("Validate LVMCluster reconciliation", func() {
		It("Should check that LVMO resources have been created", func() {
			By("Checking that CSIDriver has been created")
			err := validateCSIDriver(ctx)
			Expect(err).To(BeNil())

			By("Checking that the topolvm-controller deployment has been created")
			err = validateTopolvmController(ctx)
			Expect(err).To(BeNil())

			By("Checking that the vg-manager daemonset has been created")
			err = validateVGManager(ctx)
			Expect(err).To(BeNil())

			By("Checking that the LVMVolumeGroup has been created")
			err = validateLVMvg(ctx)
			Expect(err).To(BeNil())

			By("Checking that the topolvm-node daemonset has been created")
			err = validateTopolvmNode(ctx)
			Expect(err).To(BeNil())

			By("Checking that the StorageClass has been created")
			err = validateStorageClass(ctx)
			Expect(err).To(BeNil())

			By("Checking that the VolumeSnapshotClass has been created")
			err = validateVolumeSnapshotClass(ctx)
			Expect(err).To(BeNil())
		})
	})
}
