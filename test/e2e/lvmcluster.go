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

	v1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilwait "k8s.io/apimachinery/pkg/util/wait"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	lvmClusterName string = "rh-lvmcluster"
)

func generateLVMCluster() *v1alpha1.LVMCluster {
	lvmClusterRes := &v1alpha1.LVMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lvmClusterName,
			Namespace: installNamespace,
		},
		Spec: v1alpha1.LVMClusterSpec{
			Storage: v1alpha1.Storage{
				DeviceClasses: []v1alpha1.DeviceClass{
					{
						Name:    "vg1",
						Default: true,
						ThinPoolConfig: &v1alpha1.ThinPoolConfig{
							Name:               "mytp1",
							SizePercent:        90,
							OverprovisionRatio: 5,
						},
					},
				},
			},
		},
	}
	return lvmClusterRes
}

// startLVMCluster creates a sample CR.
func startLVMCluster(clusterConfig *v1alpha1.LVMCluster, ctx context.Context) error {
	return crClient.Create(ctx, clusterConfig)
}

// deleteLVMCluster deletes a sample CR.
func deleteLVMCluster(clusterConfig *v1alpha1.LVMCluster, ctx context.Context) error {
	lvmClusterRes := generateLVMCluster()
	cluster := &v1alpha1.LVMCluster{}
	err := crClient.Delete(ctx, lvmClusterRes)
	if err != nil {
		return err
	}

	timeout := 600 * time.Second
	interval := 10 * time.Second

	// wait for LVMCluster to be deleted
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		err = crClient.Get(ctx, types.NamespacedName{Name: lvmClusterRes.Name, Namespace: installNamespace}, cluster)
		if err != nil && errors.IsNotFound(err) {
			return true, nil
		}
		if err == nil {
			return false, nil
		}
		return true, nil
	})

	return err
}

func setupPodAndPVC() (*corev1.PersistentVolumeClaim, *corev1.Pod) {
	filePvcYaml := fmt.Sprintf(pvcYAMLTemplate, "lvmfilepvc", testNamespace, "Filesystem", storageClassName)
	pvc, err := getPVC(filePvcYaml)
	Expect(err).To(BeNil())

	err = crClient.Create(context.Background(), pvc)
	Expect(err).To(BeNil())

	podVolumeMountYaml := fmt.Sprintf(podVolumeFSYAMLTemplate, "lvmfilepod", testNamespace, "lvmfilepvc")
	pod, err := getPod(podVolumeMountYaml)
	Expect(err).To(BeNil())

	err = crClient.Create(context.Background(), pod)
	Expect(err).To(BeNil())

	return pvc, pod
}

func cleanupPVCAndPod(pvc *corev1.PersistentVolumeClaim, pod *corev1.Pod) {
	err := crClient.Delete(context.Background(), pod)
	Expect(err).To(BeNil())
	fmt.Printf("Pod %s is deleted\n", pod.Name)

	err = crClient.Delete(context.Background(), pvc)
	Expect(err).To(BeNil())
	fmt.Printf("PVC %s is deleted\n", pvc.Name)
}

func lvmClusterTest() {
	Describe("Filesystem Type", Serial, func() {

		var clusterConfig *v1alpha1.LVMCluster
		ctx := context.Background()

		AfterEach(func() {
			// Delete the cluster
			lvmClusterCleanup(clusterConfig, ctx)
		})

		It("should default to xfs", func() {
			clusterConfig = generateLVMCluster() // Do not specify a fstype

			By("Setting up the cluster with the default fstype")
			lvmClusterSetup(clusterConfig, ctx)

			// Make sure the storage class was configured properly
			sc := storagev1.StorageClass{}

			Eventually(func() bool {
				err := crClient.Get(ctx, types.NamespacedName{Name: storageClassName, Namespace: installNamespace}, &sc)
				if err != nil {
					debug("Error getting StorageClass %s: %s\n", storageClassName, err.Error())
				}
				return err == nil
			}, timeout, interval).Should(BeTrue())

			Expect(sc.Parameters["csi.storage.k8s.io/fstype"]).To(Equal(string(v1alpha1.FilesystemTypeXFS)))
		})

		It("should be xfs if specified", func() {
			clusterConfig = generateLVMCluster()
			clusterConfig.Spec.Storage.DeviceClasses[0].FilesystemType = v1alpha1.FilesystemTypeXFS

			By("Setting up the cluster with the default fstype")
			lvmClusterSetup(clusterConfig, ctx)

			// Make sure the storage class was configured properly
			sc := storagev1.StorageClass{}

			Eventually(func() bool {
				err := crClient.Get(ctx, types.NamespacedName{Name: storageClassName, Namespace: installNamespace}, &sc)
				if err != nil {
					debug("Error getting StorageClass %s: %s\n", storageClassName, err.Error())
				}
				return err == nil
			}, timeout, interval).Should(BeTrue())

			Expect(sc.Parameters["csi.storage.k8s.io/fstype"]).To(Equal(string(v1alpha1.FilesystemTypeXFS)))
		})

		It("should be ext4 if specified", func() {
			clusterConfig = generateLVMCluster()
			clusterConfig.Spec.Storage.DeviceClasses[0].FilesystemType = v1alpha1.FilesystemTypeExt4

			By("Setting up the cluster with the ext4 fstype")
			lvmClusterSetup(clusterConfig, ctx)

			// Make sure the storage class was configured properly
			sc := storagev1.StorageClass{}

			Eventually(func() bool {
				err := crClient.Get(ctx, types.NamespacedName{Name: storageClassName, Namespace: installNamespace}, &sc)
				if err != nil {
					debug("Error getting StorageClass %s: %s\n", storageClassName, err.Error())
				}
				return err == nil
			}, timeout, interval).Should(BeTrue())

			Expect(sc.Parameters["csi.storage.k8s.io/fstype"]).To(Equal(string(v1alpha1.FilesystemTypeExt4)))
		})
	})
}
