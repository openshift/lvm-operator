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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	ginkgotypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"

	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
)

func storageClassOptionsTest() {
	scName := types.NamespacedName{Name: storageClassName, Namespace: installNamespace}

	// clusterLifecycle provides per-test BeforeEach with DeferCleanup for groups
	// that create real LVMClusters. Using DeferCleanup (instead of AfterEach)
	// ensures LIFO ordering: PVC/pod cleanups registered later in It blocks run
	// first, so the cluster deletion gate is not blocked by active PVCs.
	clusterLifecycle := func(cluster **v1alpha1.LVMCluster) {
		BeforeEach(func(ctx SpecContext) {
			*cluster = GetDefaultTestLVMClusterTemplate()
			DeferCleanup(func(ctx SpecContext) {
				if CurrentSpecReport().State.Is(ginkgotypes.SpecStateFailureStates) {
					skipSuiteCleanup.Store(true)
				}
				DeleteResource(ctx, *cluster)
				validateCSINodeInfo(ctx, *cluster, false)
			})
		})
	}

	// Group 1: StorageClass Property Verification
	Describe("StorageClass Property Verification", Serial, func() {
		var cluster *v1alpha1.LVMCluster
		clusterLifecycle(&cluster)

		It("should use defaults when storageClassOptions is empty", func(ctx SpecContext) {
			cluster.Spec.Storage.DeviceClasses[0].StorageClassOptions = &v1alpha1.StorageClassOptions{}
			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)

			sc := GetStorageClass(ctx, scName)
			Expect(sc.ReclaimPolicy).ToNot(BeNil())
			Expect(*sc.ReclaimPolicy).To(Equal(corev1.PersistentVolumeReclaimDelete))
			Expect(sc.VolumeBindingMode).ToNot(BeNil())
			Expect(*sc.VolumeBindingMode).To(Equal(storagev1.VolumeBindingWaitForFirstConsumer))
		})

		It("should apply additionalLabels on StorageClass", func(ctx SpecContext) {
			cluster.Spec.Storage.DeviceClasses[0].StorageClassOptions = &v1alpha1.StorageClassOptions{
				AdditionalLabels: map[string]string{
					"environment": "production",
					"team":        "sno",
				},
			}
			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)

			sc := GetStorageClass(ctx, scName)
			Expect(sc.Labels).To(HaveKeyWithValue("environment", "production"))
			Expect(sc.Labels).To(HaveKeyWithValue("team", "sno"))
			Expect(sc.Labels).To(HaveKey("owned-by.topolvm.io/name"))
		})
	})

	// Group 2: Webhook Validation
	// These tests expect creation to be rejected, so no cluster lifecycle is needed.
	Describe("Webhook Validation", Serial, func() {
		DescribeTable("should reject invalid StorageClassOptions",
			func(ctx SpecContext, opts *v1alpha1.StorageClassOptions, errSubstring string) {
				cluster := GetDefaultTestLVMClusterTemplate()
				cluster.Spec.Storage.DeviceClasses[0].StorageClassOptions = opts
				err := crClient.Create(ctx, cluster)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(errSubstring))
			},
			Entry("LVMS-owned parameter key", &v1alpha1.StorageClassOptions{
				AdditionalParameters: map[string]string{"topolvm.io/device-class": "override"},
			}, "managed by LVMS"),
			Entry("reserved label key", &v1alpha1.StorageClassOptions{
				AdditionalLabels: map[string]string{"app.kubernetes.io/managed-by": "someone-else"},
			}, "operator-reserved"),
			Entry("invalid label value", &v1alpha1.StorageClassOptions{
				AdditionalLabels: map[string]string{"valid-key": "invalid value with spaces!"},
			}, "invalid"),
		)
	})

	// Group 3: XValidation Immutability
	// Uses Ordered + BeforeAll so all immutability tests share one cluster.
	// Rejected updates don't change server state, so the cluster stays clean.
	Describe("XValidation Immutability", Serial, Ordered, func() {
		var cluster *v1alpha1.LVMCluster

		BeforeAll(func(ctx SpecContext) {
			cluster = GetDefaultTestLVMClusterTemplate()
			cluster.Spec.Storage.DeviceClasses[0].StorageClassOptions = &v1alpha1.StorageClassOptions{
				ReclaimPolicy:     ptr.To(corev1.PersistentVolumeReclaimDelete),
				VolumeBindingMode: ptr.To(storagev1.VolumeBindingWaitForFirstConsumer),
				AdditionalParameters: map[string]string{
					"param-a": "value-a",
				},
				AdditionalLabels: map[string]string{
					"environment": "staging",
				},
			}
			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)
		})

		AfterAll(func(ctx SpecContext) {
			if CurrentSpecReport().State.Is(ginkgotypes.SpecStateFailureStates) {
				skipSuiteCleanup.Store(true)
			}
			DeleteResource(ctx, cluster)
			validateCSINodeInfo(ctx, cluster, false)
		})

		DescribeTable("should reject immutable field change",
			func(ctx SpecContext, mutate func(*v1alpha1.StorageClassOptions)) {
				Expect(crClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
				mutate(cluster.Spec.Storage.DeviceClasses[0].StorageClassOptions)
				err := crClient.Update(ctx, cluster)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("immutable"))
			},
			Entry("reclaimPolicy", func(opts *v1alpha1.StorageClassOptions) {
				opts.ReclaimPolicy = ptr.To(corev1.PersistentVolumeReclaimRetain)
			}),
			Entry("volumeBindingMode", func(opts *v1alpha1.StorageClassOptions) {
				opts.VolumeBindingMode = ptr.To(storagev1.VolumeBindingImmediate)
			}),
			Entry("additionalParameters", func(opts *v1alpha1.StorageClassOptions) {
				opts.AdditionalParameters["param-b"] = "value-b"
			}),
		)

		It("should allow additionalLabels change after creation", func(ctx SpecContext) {
			Expect(crClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
			cluster.Spec.Storage.DeviceClasses[0].StorageClassOptions.AdditionalLabels["team"] = "platform"
			Expect(crClient.Update(ctx, cluster)).To(Succeed())
		})
	})

	// Group 4: Functional Provisioning with Custom Options
	Describe("Functional Provisioning", Serial, func() {
		Describe("Immediate Binding", Serial, func() {
			var cluster *v1alpha1.LVMCluster
			clusterLifecycle(&cluster)

			It("should provision PVC without consumer, mount in pod, and verify data", func(ctx SpecContext) {
				cluster.Spec.Storage.DeviceClasses[0].StorageClassOptions = &v1alpha1.StorageClassOptions{
					VolumeBindingMode: ptr.To(storagev1.VolumeBindingImmediate),
				}
				CreateResource(ctx, cluster)
				VerifyLVMSSetup(ctx, cluster)

				By("Verifying SC has volumeBindingMode=Immediate")
				sc := GetStorageClass(ctx, scName)
				Expect(sc.VolumeBindingMode).ToNot(BeNil())
				Expect(*sc.VolumeBindingMode).To(Equal(storagev1.VolumeBindingImmediate))

				pvc := generatePVC(corev1.PersistentVolumeFilesystem)
				CreateResource(ctx, pvc)
				DeferCleanup(func(ctx SpecContext) {
					DeleteResource(ctx, pvc)
				})

				validatePVCIsBound(ctx, client.ObjectKeyFromObject(pvc))

				pod := generatePodConsumingPVC(pvc)
				CreateResource(ctx, pod)
				DeferCleanup(func(ctx SpecContext) {
					DeleteResource(ctx, pod)
				})

				validatePodIsRunning(ctx, client.ObjectKeyFromObject(pod))

				By("Writing and reading data to verify the volume works")
				expectedData := "sc-options-test"
				Expect(contentTester.WriteDataInPod(ctx, pod, expectedData, ContentModeFile)).To(Succeed())
				validatePodData(ctx, pod, expectedData, ContentModeFile)
			})
		})

		Describe("WaitForFirstConsumer Binding", Serial, func() {
			var cluster *v1alpha1.LVMCluster
			clusterLifecycle(&cluster)

			It("should delay PVC binding until consumer pod is created", func(ctx SpecContext) {
				cluster.Spec.Storage.DeviceClasses[0].StorageClassOptions = &v1alpha1.StorageClassOptions{
					VolumeBindingMode: ptr.To(storagev1.VolumeBindingWaitForFirstConsumer),
				}
				CreateResource(ctx, cluster)
				VerifyLVMSSetup(ctx, cluster)

				pvc := generatePVC(corev1.PersistentVolumeFilesystem)
				CreateResource(ctx, pvc)
				DeferCleanup(func(ctx SpecContext) {
					DeleteResource(ctx, pvc)
				})

				By("Verifying PVC stays unbound without a consumer")
				Consistently(func(ctx SpecContext) corev1.PersistentVolumeClaimPhase {
					p := &corev1.PersistentVolumeClaim{}
					if err := crClient.Get(ctx, client.ObjectKeyFromObject(pvc), p); err != nil {
						return ""
					}
					return p.Status.Phase
				}, 15*time.Second, interval).WithContext(ctx).ShouldNot(Equal(corev1.ClaimBound))

				By("Creating a consumer pod to trigger binding")
				pod := generatePodConsumingPVC(pvc)
				CreateResource(ctx, pod)
				DeferCleanup(func(ctx SpecContext) {
					DeleteResource(ctx, pod)
				})

				validatePodIsRunning(ctx, client.ObjectKeyFromObject(pod))
				validatePVCIsBound(ctx, client.ObjectKeyFromObject(pvc))
			})
		})

		Describe("Retain Policy Deletion Lifecycle", Serial, func() {
			It("should block deletion with Retain PVs and unblock after LV cleanup", func(ctx SpecContext) {
				cluster := GetDefaultTestLVMClusterTemplate()
				cluster.Spec.Storage.DeviceClasses[0].StorageClassOptions = &v1alpha1.StorageClassOptions{
					ReclaimPolicy:     ptr.To(corev1.PersistentVolumeReclaimRetain),
					VolumeBindingMode: ptr.To(storagev1.VolumeBindingImmediate),
				}
				CreateResource(ctx, cluster)
				VerifyLVMSSetup(ctx, cluster)

				By("Verifying SC has reclaimPolicy=Retain")
				sc := GetStorageClass(ctx, scName)
				Expect(sc.ReclaimPolicy).ToNot(BeNil())
				Expect(*sc.ReclaimPolicy).To(Equal(corev1.PersistentVolumeReclaimRetain))

				var pvName string

				DeferCleanup(func(ctx SpecContext) {
					if CurrentSpecReport().State.Is(ginkgotypes.SpecStateFailureStates) {
						skipSuiteCleanup.Store(true)
					}
					deleteLogicalVolumes(ctx)
					DeleteResource(ctx, cluster)
					validateCSINodeInfo(ctx, cluster, false)
				})

				pvc := generatePVC(corev1.PersistentVolumeFilesystem)
				CreateResource(ctx, pvc)

				validatePVCIsBound(ctx, client.ObjectKeyFromObject(pvc))

				Expect(crClient.Get(ctx, client.ObjectKeyFromObject(pvc), pvc)).To(Succeed())
				pvName = pvc.Spec.VolumeName
				Expect(pvName).ToNot(BeEmpty(), "bound PVC should have a volume name")

				By("Deleting the PVC — PV must remain due to Retain policy")
				Expect(crClient.Delete(ctx, pvc)).To(Succeed())
				Eventually(func(ctx SpecContext) error {
					return crClient.Get(ctx, client.ObjectKeyFromObject(pvc), pvc)
				}, timeout, interval).WithContext(ctx).Should(Satisfy(k8serrors.IsNotFound))

				pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: pvName}}
				Expect(crClient.Get(ctx, client.ObjectKeyFromObject(pv), pv)).To(Succeed(),
					"PV should still exist after PVC deletion (Retain policy)")
				Expect(pv.Spec.PersistentVolumeReclaimPolicy).To(Equal(corev1.PersistentVolumeReclaimRetain),
					"PV should have inherited Retain reclaim policy from StorageClass")

				By("Requesting LVMCluster deletion")
				Expect(crClient.Delete(ctx, cluster)).To(Succeed())

				By("Verifying LVMCluster deletion is blocked by the Retain PV gate")
				Consistently(func(ctx SpecContext) bool {
					if err := crClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster); err != nil {
						return false
					}
					return !cluster.DeletionTimestamp.IsZero()
				}, 15*time.Second, interval).WithContext(ctx).Should(BeTrue(),
					"LVMCluster should still exist with deletionTimestamp while Retain PV is present")

				Eventually(func(ctx SpecContext) bool {
					return hasEventWithReason(ctx, installNamespace, cluster.Name, "DeletionPending", "Retain")
				}, timeout, interval).WithContext(ctx).Should(BeTrue())

				By("Deleting the retained PV")
				Expect(crClient.Delete(ctx, pv)).To(Succeed())
				Eventually(func(ctx SpecContext) error {
					return crClient.Get(ctx, client.ObjectKeyFromObject(pv), pv)
				}, timeout, interval).WithContext(ctx).Should(Satisfy(k8serrors.IsNotFound))

				By("Verifying LVMCluster deletion is still blocked (on-disk LVs remain)")
				Consistently(func(ctx SpecContext) bool {
					if err := crClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster); err != nil {
						return false
					}
					return !cluster.DeletionTimestamp.IsZero()
				}, 20*time.Second, interval).WithContext(ctx).Should(BeTrue(),
					"LVMCluster should still exist with deletionTimestamp while on-disk LVs remain")

				Eventually(func(ctx SpecContext) bool {
					return hasEventWithReason(ctx, installNamespace, "", "ManualCleanupRequired", "")
				}, timeout, interval).WithContext(ctx).Should(BeTrue())

				By("Deleting LogicalVolume CRs to trigger on-disk LV cleanup")
				deleteLogicalVolumes(ctx)

				By("Waiting for LVMCluster to be fully deleted")
				Eventually(func(ctx SpecContext) error {
					return crClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
				}, 5*time.Minute, interval).WithContext(ctx).Should(Satisfy(k8serrors.IsNotFound))
			})
		})

		Describe("Additional Parameters", Serial, func() {
			var cluster *v1alpha1.LVMCluster
			clusterLifecycle(&cluster)

			It("should provision and mount PVC with custom SC parameters", func(ctx SpecContext) {
				cluster.Spec.Storage.DeviceClasses[0].StorageClassOptions = &v1alpha1.StorageClassOptions{
					VolumeBindingMode: ptr.To(storagev1.VolumeBindingImmediate),
					AdditionalParameters: map[string]string{
						"custom-param": "custom-value",
					},
				}
				CreateResource(ctx, cluster)
				VerifyLVMSSetup(ctx, cluster)

				By("Verifying SC has the additional parameters alongside LVMS-owned keys")
				sc := GetStorageClass(ctx, scName)
				Expect(sc.Parameters).To(HaveKeyWithValue("custom-param", "custom-value"))
				Expect(sc.Parameters).To(HaveKey("topolvm.io/device-class"))
				Expect(sc.Parameters).To(HaveKey("csi.storage.k8s.io/fstype"))

				pvc := generatePVC(corev1.PersistentVolumeFilesystem)
				CreateResource(ctx, pvc)
				DeferCleanup(func(ctx SpecContext) {
					DeleteResource(ctx, pvc)
				})

				validatePVCIsBound(ctx, client.ObjectKeyFromObject(pvc))

				pod := generatePodConsumingPVC(pvc)
				CreateResource(ctx, pod)
				DeferCleanup(func(ctx SpecContext) {
					DeleteResource(ctx, pod)
				})

				validatePodIsRunning(ctx, client.ObjectKeyFromObject(pod))
			})
		})
	})

	// Group 5: SSA Label Pruning
	Describe("SSA Label Pruning", Serial, func() {
		var cluster *v1alpha1.LVMCluster
		clusterLifecycle(&cluster)

		It("should prune removed additionalLabel from StorageClass", func(ctx SpecContext) {
			cluster.Spec.Storage.DeviceClasses[0].StorageClassOptions = &v1alpha1.StorageClassOptions{
				AdditionalLabels: map[string]string{
					"environment": "production",
					"team":        "sno",
				},
			}
			CreateResource(ctx, cluster)
			VerifyLVMSSetup(ctx, cluster)

			Eventually(func(ctx SpecContext) map[string]string {
				sc := GetStorageClass(ctx, scName)
				return sc.Labels
			}, timeout, interval).WithContext(ctx).Should(And(
				HaveKeyWithValue("environment", "production"),
				HaveKeyWithValue("team", "sno"),
			))

			By("Removing the 'team' label from the cluster")
			Expect(crClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
			cluster.Spec.Storage.DeviceClasses[0].StorageClassOptions.AdditionalLabels = map[string]string{
				"environment": "production",
			}
			Expect(crClient.Update(ctx, cluster)).To(Succeed())

			By("Verifying the 'team' label was pruned from the StorageClass via SSA")
			Eventually(func(ctx SpecContext) map[string]string {
				sc := &storagev1.StorageClass{}
				err := crClient.Get(ctx, scName, sc)
				if err != nil {
					return nil
				}
				return sc.Labels
			}, timeout, interval).WithContext(ctx).Should(And(
				HaveKeyWithValue("environment", "production"),
				Not(HaveKey("team")),
				HaveKey("owned-by.topolvm.io/name"),
			))
		})
	})
}

// hasEventWithReason checks whether a Kubernetes event with the given reason exists
// for the specified object in the namespace. If messageSubstring is non-empty, the
// event message must contain it. If objectName is empty, any object in the namespace matches.
func hasEventWithReason(ctx SpecContext, namespace, objectName, reason, messageSubstring string) bool {
	GinkgoHelper()
	eventList := &corev1.EventList{}
	opts := &client.ListOptions{
		Namespace:     namespace,
		FieldSelector: fields.OneTermEqualSelector("reason", reason),
	}
	if err := crClient.List(ctx, eventList, opts); err != nil {
		return false
	}
	for _, event := range eventList.Items {
		if objectName != "" && event.InvolvedObject.Name != objectName {
			continue
		}
		if messageSubstring != "" && !strings.Contains(event.Message, messageSubstring) {
			continue
		}
		return true
	}
	return false
}

// deleteLogicalVolumes removes TopoLVM LogicalVolume CRs for the test device
// class via the Kubernetes API. Deleting these CRs triggers TopoLVM's node
// controller to remove the underlying on-disk LVs through its finalizer,
// which unblocks the VGManager ManualCleanupRequired gate.
func deleteLogicalVolumes(ctx SpecContext) {
	GinkgoHelper()
	lvList := &topolvmv1.LogicalVolumeList{}
	Expect(crClient.List(ctx, lvList)).To(Succeed())

	for i := range lvList.Items {
		lv := &lvList.Items[i]
		if lv.Spec.DeviceClass != lvmVolumeGroupName {
			continue
		}
		Expect(client.IgnoreNotFound(crClient.Delete(ctx, lv))).To(Succeed())
	}

	Eventually(func(ctx SpecContext) bool {
		lvList := &topolvmv1.LogicalVolumeList{}
		if err := crClient.List(ctx, lvList); err != nil {
			return false
		}
		for _, lv := range lvList.Items {
			if lv.Spec.DeviceClass == lvmVolumeGroupName {
				return false
			}
		}
		return true
	}, timeout, interval).WithContext(ctx).Should(BeTrue())
}
