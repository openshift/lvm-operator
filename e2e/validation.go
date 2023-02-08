/*
Copyright 2022 Red Hat Openshift Data Foundation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	. "github.com/onsi/gomega"
	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	timeout                   = time.Minute * 10
	interval                  = time.Second * 15
	lvmVolumeGroupName        = "vg1"
	storageClassName          = "lvms-vg1"
	volumeSnapshotClassName   = "lvms-vg1"
	csiDriverName             = "topolvm.io"
	topolvmNodeDaemonSetName  = "topolvm-node"
	topolvmCtrlDeploymentName = "topolvm-controller"
	vgManagerDaemonsetName    = "vg-manager"
)

// function to validate LVMVolume group.
func validateLVMvg(ctx context.Context) error {
	lvmVG := lvmv1alpha1.LVMVolumeGroup{}

	Eventually(func() bool {
		err := crClient.Get(ctx, types.NamespacedName{Name: lvmVolumeGroupName, Namespace: installNamespace}, &lvmVG)
		if err != nil {
			debug("Error getting LVMVolumeGroup %s: %s\n", lvmVolumeGroupName, err.Error())
		}
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
		if err != nil {
			debug("Error getting StorageClass %s: %s\n", storageClassName, err.Error())
		}
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
		if err != nil {
			debug("Error getting VolumeSnapshotClass %s: %s\n", volumeSnapshotClassName, err.Error())
		}
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
		if err != nil {
			debug("Error getting CSIDriver %s: %s\n", csiDriverName, err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("CSIDriver found\n")
	return nil
}

// function to validate TopoLVM node.
func validateTopolvmNode(ctx context.Context) error {
	ds := appsv1.DaemonSet{}
	Eventually(func() bool {
		err := crClient.Get(ctx, types.NamespacedName{Name: topolvmNodeDaemonSetName, Namespace: installNamespace}, &ds)
		if err != nil {
			debug("Error getting TopoLVM node daemonset %s: %s\n", topolvmNodeDaemonSetName, err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())
	debug("TopoLVM node daemonset found\n")

	// checking for the ready status
	Eventually(func() bool {
		err := crClient.Get(ctx, types.NamespacedName{Name: topolvmNodeDaemonSetName, Namespace: installNamespace}, &ds)
		if err != nil {
			debug("Error getting TopoLVM node daemonset %s: %s\n", topolvmNodeDaemonSetName, err.Error())
		}
		return ds.Status.DesiredNumberScheduled == ds.Status.NumberReady
	}, timeout, interval).Should(BeTrue())
	debug("TopoLVM node pods are ready\n")

	return nil
}

// function to validate vg manager resource.
func validateVGManager(ctx context.Context) error {
	ds := appsv1.DaemonSet{}

	Eventually(func() bool {
		err := crClient.Get(ctx, types.NamespacedName{Name: vgManagerDaemonsetName, Namespace: installNamespace}, &ds)
		if err != nil {
			debug("Error getting vg manager daemonset %s: %s\n", vgManagerDaemonsetName, err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())
	debug("VG manager daemonset found\n")

	return nil
}

// function to validate TopoLVM Deployment.
func validateTopolvmController(ctx context.Context) error {
	dep := appsv1.Deployment{}

	Eventually(func() bool {
		err := crClient.Get(ctx, types.NamespacedName{Name: topolvmCtrlDeploymentName, Namespace: installNamespace}, &dep)
		if err != nil {
			debug("Error getting TopoLVM controller deployment %s: %s\n", topolvmCtrlDeploymentName, err.Error())
		}
		return err == nil
	}, timeout, interval).Should(BeTrue())

	debug("TopoLVM controller deployment found\n")
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
