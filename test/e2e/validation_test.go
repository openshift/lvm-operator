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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"

	appsv1 "k8s.io/api/apps/v1"
	k8sv1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
)

const (
	timeout                 = time.Minute * 2
	interval                = time.Millisecond * 300
	lvmVolumeGroupName      = "vg1"
	storageClassName        = "lvms-vg1"
	volumeSnapshotClassName = "lvms-vg1"
	csiDriverName           = "topolvm.io"
	vgManagerDaemonsetName  = "vg-manager"
)

func validateLVMCluster(ctx context.Context, cluster *v1alpha1.LVMCluster) bool {
	GinkgoHelper()
	checkClusterIsReady := func(ctx context.Context) error {
		currentCluster := cluster
		err := crClient.Get(ctx, client.ObjectKeyFromObject(cluster), currentCluster)
		if err != nil {
			return err
		}
		if currentCluster.Status.State == v1alpha1.LVMStatusReady {
			return nil
		}
		return fmt.Errorf("cluster is not ready: %v", currentCluster.Status)
	}
	By("validating the LVMCluster")
	return Eventually(checkClusterIsReady, timeout, interval).WithContext(ctx).Should(Succeed())
}

// function to validate LVMVolume group.
func validateLVMVolumeGroup(ctx context.Context) bool {
	GinkgoHelper()
	By("validating the LVMVolumeGroup")
	return Eventually(func(ctx context.Context) error {
		return crClient.Get(ctx, types.NamespacedName{Name: lvmVolumeGroupName, Namespace: installNamespace}, &v1alpha1.LVMVolumeGroup{})
	}, timeout, interval).WithContext(ctx).Should(Succeed())
}

// function to validate storage class.
func validateStorageClass(ctx context.Context) bool {
	GinkgoHelper()
	By("validating the StorageClass")
	return Eventually(func() error {
		return crClient.Get(ctx, types.NamespacedName{Name: storageClassName, Namespace: installNamespace}, &storagev1.StorageClass{})
	}, timeout, interval).WithContext(ctx).Should(Succeed())
}

// function to validate volume snapshot class.
func validateVolumeSnapshotClass(ctx context.Context) bool {
	GinkgoHelper()
	By("validating the VolumeSnapshotClass")
	return Eventually(func(ctx context.Context) error {
		err := crClient.Get(ctx, types.NamespacedName{Name: volumeSnapshotClassName}, &snapapi.VolumeSnapshotClass{})
		if meta.IsNoMatchError(err) {
			GinkgoLogr.Info("VolumeSnapshotClass is ignored since VolumeSnapshotClasses are not supported in the given Cluster")
			return nil
		}
		return err
	}, timeout, interval).WithContext(ctx).Should(Succeed())
}

// function to validate CSI Driver.
func validateCSIDriver(ctx context.Context) bool {
	GinkgoHelper()
	By("validating the CSIDriver")
	return Eventually(func(ctx context.Context) error {
		return crClient.Get(ctx, types.NamespacedName{Name: csiDriverName, Namespace: installNamespace}, &storagev1.CSIDriver{})
	}, timeout, interval).WithContext(ctx).Should(Succeed())
}

// function to validate vg manager resource.
func validateVGManager(ctx context.Context) bool {
	GinkgoHelper()
	By("validating the vg-manager DaemonSet")
	return validateDaemonSet(ctx, types.NamespacedName{Name: vgManagerDaemonsetName, Namespace: installNamespace})
}

func validateDaemonSet(ctx context.Context, name types.NamespacedName) bool {
	GinkgoHelper()
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

func validatePVCIsBound(ctx context.Context, name types.NamespacedName) bool {
	GinkgoHelper()
	By(fmt.Sprintf("validating the PVC %q", name))
	return Eventually(func(ctx context.Context) error {
		pvc := &k8sv1.PersistentVolumeClaim{}
		if err := crClient.Get(ctx, name, pvc); err != nil {
			return err
		}
		if pvc.Status.Phase != k8sv1.ClaimBound {
			return fmt.Errorf("pvc is not bound yet: %s", pvc.Status.Phase)
		}
		return nil
	}, timeout, interval).WithContext(ctx).Should(Succeed(), "pvc should be bound")
}

func validatePodIsRunning(ctx context.Context, name types.NamespacedName) bool {
	GinkgoHelper()
	By(fmt.Sprintf("validating the Pod %q", name))
	return Eventually(func(ctx context.Context) bool {
		pod := &k8sv1.Pod{}
		err := crClient.Get(ctx, name, pod)
		return err == nil && pod.Status.Phase == k8sv1.PodRunning
	}, timeout, interval).WithContext(ctx).Should(BeTrue(), "pod should be running")
}

func validateSnapshotReadyToUse(ctx context.Context, name types.NamespacedName) bool {
	GinkgoHelper()
	By(fmt.Sprintf("validating the VolumeSnapshot %q", name))
	return Eventually(func(ctx context.Context) bool {
		snapshot := &snapapi.VolumeSnapshot{}
		err := crClient.Get(ctx, name, snapshot)
		if err == nil && snapshot.Status != nil && snapshot.Status.ReadyToUse != nil {
			return *snapshot.Status.ReadyToUse
		}
		return false
	}, timeout, interval).WithContext(ctx).Should(BeTrue())
}

func validatePodData(ctx context.Context, pod *k8sv1.Pod, expectedData string, contentMode ContentMode) bool {
	var actualData string
	By(fmt.Sprintf("validating the Data written in Pod %q", client.ObjectKeyFromObject(pod)))
	Eventually(func(ctx context.Context) error {
		var err error
		actualData, err = contentTester.GetDataInPod(ctx, pod, contentMode)
		return err
	}).WithContext(ctx).Should(Succeed())
	return Expect(actualData).To(Equal(expectedData))
}
