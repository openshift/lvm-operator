package e2e

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	timeout                   = time.Minute * 15
	interval                  = time.Second * 30
	lvmVolumeGroupName        = "vg1"
	storageClassName          = "odf-lvm-vg1"
	csiDriverName             = "topolvm.cybozu.com"
	topolvmNodeDaemonSetName  = "topolvm-node"
	topolvmCtrlDeploymentName = "topolvm-controller"
	vgManagerDaemonsetName    = "vg-manager"
)

// function to validate LVMVolume group.
func ValidateLVMvg() error {
	lvmVG := lvmv1alpha1.LVMVolumeGroup{}

	Eventually(func() bool {
		debug("%s \n", "Starting function - vg")
		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: lvmVolumeGroupName, Namespace: InstallNamespace}, &lvmVG)
		if err != nil {
			debug("LVMVolumeGroup: %s\n", err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("VG found\n")
	return nil
}

// function to validate storage class.
func ValidateStorageClass() error {
	sc := storagev1.StorageClass{}

	Eventually(func() bool {
		debug("%s\n", "Starting function - sc")
		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: storageClassName}, &sc)
		if err != nil {
			debug("StorageClass : %s\n", err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("SC found\n")
	return nil
}

// function to validate CSI Driver.
func ValidateCSIDriver() error {
	cd := storagev1.CSIDriver{}

	Eventually(func() bool {
		debug("%s\n", "Starting function - cd")
		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: csiDriverName, Namespace: InstallNamespace}, &cd)
		if err != nil {
			debug("CSIDriver : %s\n", err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("CSI Driver found\n")
	return nil
}

// function to validate TopoLVM node.
func ValidateTopolvmNode() error {
	ds := appsv1.DaemonSet{}
	Eventually(func() bool {
		debug("%s\n", "Starting function - topolvmnode")
		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: topolvmNodeDaemonSetName, Namespace: InstallNamespace}, &ds)
		if err != nil {
			debug("topolvmNode : %s\n", err.Error())
			return false
		}
		return ds.Status.DesiredNumberScheduled == ds.Status.NumberReady
	}, timeout, interval).Should(BeTrue())
	debug("TopoLVM node found\n")

	/* 	// checking for the ready status
	   	Eventually(func() bool {
	   		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: topolvmNodeDaemonSetName, Namespace: InstallNamespace}, &ds)
	   		if err != nil {
	   			debug("topolvmNode : %s", err.Error())
	   			return
	   		}
	   		return ds.Status.DesiredNumberScheduled == ds.Status.NumberReady
	   	}, timeout, interval).Should(BeTrue())
	   	debug("Status is ready\n") */

	return nil
}

// function to validate vg manager resource.
func ValidateVGManager() error {
	ds := appsv1.DaemonSet{}

	Eventually(func() bool {
		debug("%s\n", "Starting function - vgmanager")
		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: vgManagerDaemonsetName, Namespace: InstallNamespace}, &ds)
		if err != nil {
			debug("vgmanager : %s\n", err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())
	debug("VG manager found\n")

	return nil
}

// function to validate TopoLVM Deployment.
func ValidateTopolvmController() error {
	dep := appsv1.Deployment{}

	Eventually(func() bool {
		debug("%s\n", "Starting function - topolvmcontroller")
		err := DeployManagerObj.GetCrClient().Get(context.TODO(), types.NamespacedName{Name: topolvmCtrlDeploymentName, Namespace: InstallNamespace}, &dep)
		if err != nil {
			debug("topolvmcontroller : %s\n", err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("topolvm-controller deployment found\n")
	return nil
}

// Validate all the resources created by LVMO.
func ValidateResources() error {

	obj := lvmv1alpha1.LVMCluster{}
	myclient := DeployManagerObj.GetCrClient()

	debug("%s\n", "Getting lvmcluster")
	err := myclient.Get(context.TODO(), types.NamespacedName{Name: "lvmcluster-sample", Namespace: InstallNamespace}, &obj)
	if err != nil {
		debug("lvmcluster : %s\n", err.Error())
	} else {
		debug("Found lvmcluster : %v\n", obj)

	}

	podlist := v1.PodList{}
	debug("%s\n", "Getting lvm operator pod")
	lo := &client.ListOptions{}
	client.MatchingLabels{"control-plane": "controller-manager"}.ApplyToList(lo)

	err = myclient.List(context.TODO(), &podlist, lo)
	//podname := "lvm-operator-controller"
	if err == nil {
		for _, pod := range podlist.Items {
			debug("\nPod Status %v\n", pod.Status)
		}
	}

	// Validate CSI Driver
	err = ValidateCSIDriver()
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
	return nil
}
