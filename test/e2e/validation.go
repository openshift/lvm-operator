/*
Copyright © 2023 Red Hat, Inc.

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
	"errors"
	"fmt"
	"k8s.io/client-go/discovery"
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
	timeout                   = time.Minute * 2
	interval                  = time.Second * 3
	lvmVolumeGroupName        = "vg1"
	storageClassName          = "lvms-vg1"
	volumeSnapshotClassName   = "lvms-vg1"
	csiDriverName             = "topolvm.io"
	topolvmNodeDaemonSetName  = "topolvm-node"
	topolvmCtrlDeploymentName = "topolvm-controller"
	vgManagerDaemonsetName    = "vg-manager"
)

// function to validate LVMVolume group.
func validateLVMvg(ctx context.Context) bool {
	By("validating the LVMVolumeGroup")
	return Eventually(func(ctx context.Context) error {
		return crClient.Get(ctx, types.NamespacedName{Name: lvmVolumeGroupName, Namespace: installNamespace}, &lvmv1alpha1.LVMVolumeGroup{})
	}, timeout, interval).WithContext(ctx).Should(Succeed())
}

// function to validate storage class.
func validateStorageClass(ctx context.Context) bool {
	By("validating the StorageClass")
	return Eventually(func() error {
		return crClient.Get(ctx, types.NamespacedName{Name: storageClassName, Namespace: installNamespace}, &storagev1.StorageClass{})
	}, timeout, interval).WithContext(ctx).Should(Succeed())
}

// function to validate volume snapshot class.
func validateVolumeSnapshotClass(ctx context.Context) bool {
	By("validating the VolumeSnapshotClass")
	return Eventually(func(ctx context.Context) error {
		err := crClient.Get(ctx, types.NamespacedName{Name: volumeSnapshotClassName}, &snapapi.VolumeSnapshotClass{})
		if discovery.IsGroupDiscoveryFailedError(errors.Unwrap(err)) {
			By("VolumeSnapshotClass is ignored since VolumeSnapshotClasses are not supported in the given Cluster")
			return nil
		}
		return err
	}, timeout, interval).WithContext(ctx).Should(Succeed())
}

// function to validate CSI Driver.
func validateCSIDriver(ctx context.Context) bool {
	By("validating the CSIDriver")
	return Eventually(func(ctx context.Context) error {
		return crClient.Get(ctx, types.NamespacedName{Name: csiDriverName, Namespace: installNamespace}, &storagev1.CSIDriver{})
	}, timeout, interval).WithContext(ctx).Should(Succeed())
}

// function to validate TopoLVM node.
func validateTopolvmNode(ctx context.Context) bool {
	By("validating the TopoLVM Node DaemonSet")
	return validateDaemonSet(ctx, types.NamespacedName{Name: topolvmNodeDaemonSetName, Namespace: installNamespace})
}

// function to validate vg manager resource.
func validateVGManager(ctx context.Context) bool {
	By("validating the vg-manager DaemonSet")
	return validateDaemonSet(ctx, types.NamespacedName{Name: vgManagerDaemonsetName, Namespace: installNamespace})
}

// function to validate TopoLVM Deployment.
func validateTopolvmController(ctx context.Context) bool {
	By("validating the TopoLVM controller deployment")
	name := types.NamespacedName{Name: topolvmCtrlDeploymentName, Namespace: installNamespace}
	return Eventually(func(ctx context.Context) error {
		deploy := &appsv1.Deployment{}
		if err := crClient.Get(ctx, name, deploy); err != nil {
			return err
		}
		isReady := deploy.Spec.Replicas != nil && *deploy.Spec.Replicas == deploy.Status.ReadyReplicas
		if !isReady {
			return fmt.Errorf("the Deployment %s is not considered ready", name)
		}
		return nil
	}, timeout, interval).WithContext(ctx).Should(Succeed())
}

func validateDaemonSet(ctx context.Context, name types.NamespacedName) bool {
	return Eventually(func(ctx context.Context) error {
		ds := &appsv1.DaemonSet{}
		if err := crClient.Get(ctx, name, ds); err != nil {
			return err
		}
		isReady := ds.Status.DesiredNumberScheduled == ds.Status.NumberReady
		if !isReady {
			return fmt.Errorf("the DaemonSet %s is not considered ready", name)
		}
		return nil
	}, timeout, interval).WithContext(ctx).Should(Succeed())
}

// Validate all the resources created by LVMO.
func validateResources() {
	Describe("Validate LVMCluster reconciliation", func() {
		It("Should check that LVMO resources have been created", func(ctx SpecContext) {
			By("Checking that CSIDriver has been created")
			Expect(validateCSIDriver(ctx)).To(BeTrue())

			By("Checking that the topolvm-controller deployment has been created")
			Expect(validateTopolvmController(ctx)).To(BeTrue())

			By("Checking that the vg-manager daemonset has been created")
			Expect(validateVGManager(ctx)).To(BeTrue())

			By("Checking that the LVMVolumeGroup has been created")
			Expect(validateLVMvg(ctx)).To(BeTrue())

			By("Checking that the topolvm-node daemonset has been created")
			Expect(validateTopolvmNode(ctx)).To(BeTrue())

			By("Checking that the StorageClass has been created")
			Expect(validateStorageClass(ctx)).To(BeTrue())

			By("Checking that the VolumeSnapshotClass has been created")
			Expect(validateVolumeSnapshotClass(ctx)).To(BeTrue())
		})
	})
}
