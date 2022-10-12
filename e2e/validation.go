package e2e

import (
	"context"
	"time"

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

// function to validate LVMVolume group.
func ValidateLVMvg() error {
	lvmVG := lvmv1alpha1.LVMVolumeGroup{}

	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: lvmVolumeGroupName, Namespace: InstallNamespace}, &lvmVG)
		if err != nil {
			debug("Error getting LVMVolumeGroup %s: %s\n", lvmVolumeGroupName, err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("LvmVolumeGroup found\n")
	return nil
}

// function to validate storage class.
func ValidateStorageClass() error {
	sc := storagev1.StorageClass{}

	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: storageClassName, Namespace: InstallNamespace}, &sc)
		if err != nil {
			debug("Error getting StorageClass %s: %s\n", storageClassName, err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("StorageClass found\n")
	return nil
}

// function to validate volume snapshot class.
func ValidateVolumeSnapshotClass() error {
	vsc := snapapi.VolumeSnapshotClass{}

	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: volumeSnapshotClassName}, &vsc)
		if err != nil {
			debug("Error getting VolumeSnapshotClass %s: %s\n", volumeSnapshotClassName, err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("VolumeSnapshotClass found\n")
	return nil
}

// function to validate CSI Driver.
func ValidateCSIDriver() error {
	cd := storagev1.CSIDriver{}

	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: csiDriverName, Namespace: InstallNamespace}, &cd)
		if err != nil {
			debug("Error getting CSIDriver %s: %s\n", csiDriverName, err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("CSIDriver found\n")
	return nil
}

// function to validate TopoLVM node.
func ValidateTopolvmNode() error {
	ds := appsv1.DaemonSet{}
	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: topolvmNodeDaemonSetName, Namespace: InstallNamespace}, &ds)
		if err != nil {
			debug("Error getting TopoLVM node daemonset %s: %s\n", topolvmNodeDaemonSetName, err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())
	debug("TopoLVM node daemonset found\n")

	return nil
}

// function to validate vg manager resource.
func ValidateVGManager() error {
	ds := appsv1.DaemonSet{}

	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: vgManagerDaemonsetName, Namespace: InstallNamespace}, &ds)
		if err != nil {
			debug("Error getting VG manager daemonset %s: %s\n", vgManagerDaemonsetName, err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())
	debug("VG manager daemonset found\n")

	return nil
}

// function to validate TopoLVM Deployment.
func ValidateTopolvmController() error {
	dep := appsv1.Deployment{}

	Eventually(func() bool {
		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: topolvmCtrlDeploymentName, Namespace: InstallNamespace}, &dep)
		if err != nil {
			debug("Error getting TopoLVM controller deployment %s: %s\n", topolvmCtrlDeploymentName, err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("topolvm-controller deployment found\n")
	return nil
}

// Validate all the resources created by LVMO.
func ValidateResources() error {

	// Validate CSI Driver
	err := ValidateCSIDriver()
	if err != nil {
		return err
	}

	//Validate TopoLVM Controller
	err = ValidateTopolvmController()
	if err != nil {
		return err
	}

	// Validate VG Manager Daemonset
	err = ValidateVGManager()
	if err != nil {
		return err
	}
	// Validate LVMVg
	err = ValidateLVMvg()
	if err != nil {
		return err
	}

	// Validate Topolvm node
	err = ValidateTopolvmNode()
	if err != nil {
		return err
	}
	// Validate Storage class
	err = ValidateStorageClass()
	if err != nil {
		return err
	}

	// Validate Volume Snapshot class
	err = ValidateVolumeSnapshotClass()
	if err != nil {
		return err
	}

	return nil
}
