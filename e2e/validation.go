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
	interval                  = time.Second * 30
	lvmVolumeGroupName        = "vg1"
	storageClassName          = "odf-lvm-vg1"
	volumeSnapshotClassName   = "odf-lvm-vg1"
	csiDriverName             = "topolvm.cybozu.com"
	topolvmNodeDaemonSetName  = "topolvm-node"
	topolvmCtrlDeploymentName = "topolvm-controller"
	vgManagerDaemonsetName    = "vg-manager"
)

var (
	ctx = context.TODO()
)

// function to validate LVMVolume group.
func validateLVMvg() error {
	lvmVG := lvmv1alpha1.LVMVolumeGroup{}

	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(ctx, types.NamespacedName{Name: lvmVolumeGroupName, Namespace: InstallNamespace}, &lvmVG)
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("VG found\n")
	return nil
}

// function to validate storage class.
func validateStorageClass() error {
	sc := storagev1.StorageClass{}

	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(ctx, types.NamespacedName{Name: storageClassName, Namespace: InstallNamespace}, &sc)
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("SC found\n")
	return nil
}

// function to validate volume snapshot class.
func ValidateVolumeSnapshotClass() error {
	vsc := snapapi.VolumeSnapshotClass{}

	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(ctx, types.NamespacedName{Name: volumeSnapshotClassName}, &vsc)
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("VolumeSnapshotClass found\n")
	return nil
}

// function to validate CSI Driver.
func validateCSIDriver() error {
	cd := storagev1.CSIDriver{}

	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(ctx, types.NamespacedName{Name: csiDriverName, Namespace: InstallNamespace}, &cd)
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("CSI Driver found\n")
	return nil
}

// function to validate TopoLVM node.
func validateTopolvmNode() error {
	ds := appsv1.DaemonSet{}
	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(ctx, types.NamespacedName{Name: topolvmNodeDaemonSetName, Namespace: InstallNamespace}, &ds)
		return err == nil
	}, timeout, interval).Should(BeTrue())
	debug("TopoLVM node found\n")

	// checking for the ready status
	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(ctx, types.NamespacedName{Name: topolvmNodeDaemonSetName, Namespace: InstallNamespace}, &ds)
		if err != nil {
			debug("topolvmNode : %s", err.Error())
		}
		return ds.Status.DesiredNumberScheduled == ds.Status.NumberReady
	}, timeout, interval).Should(BeTrue())
	debug("TopolvmNode Status is ready\n")

	return nil
}

// function to validate vg manager resource.
func validateVGManager() error {
	ds := appsv1.DaemonSet{}

	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(ctx, types.NamespacedName{Name: vgManagerDaemonsetName, Namespace: InstallNamespace}, &ds)
		return err == nil
	}, timeout, interval).Should(BeTrue())
	debug("VG manager found\n")

	return nil
}

// function to validate TopoLVM Deployment.
func validateTopolvmController() error {
	dep := appsv1.Deployment{}

	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(ctx, types.NamespacedName{Name: topolvmCtrlDeploymentName, Namespace: InstallNamespace}, &dep)
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("topolvm-controller deployment found\n")
	return nil
}

// Validate all the resources created by LVMO.
func ValidateResources() {

	Describe("Validate LVMCluster reconciliation", func() {
		It("Should check that LVMO resources have been created", func() {
			By("Checking that CSIDriver has been created")
			err := validateCSIDriver()
			Expect(err).To(BeNil())

			By("Checking that the topolvm-controller deployment has been created")
			err = validateTopolvmController()
			Expect(err).To(BeNil())

			By("Checking that the vg-manager daemonset has been created")
			err = validateVGManager()
			Expect(err).To(BeNil())

			By("Checking that the LVMVolumeGroup has been created")
			err = validateLVMvg()
			Expect(err).To(BeNil())

			By("Checking that the topolvm-node daemonset has been created")
			err = validateTopolvmNode()
			Expect(err).To(BeNil())

			By("Checking that the StorageClass has been created")
			err = validateStorageClass()
			Expect(err).To(BeNil())

			By("Checking that the VolumeSnapshotClass has been created")
			err = ValidateVolumeSnapshotClass()
			Expect(err).To(BeNil())
		})
	})
}
