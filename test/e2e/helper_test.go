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
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"

	k8sv1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
)

const (
	DefaultTestLVMCluster string = "rh-lvmcluster"
)

// beforeTestSuiteSetup is the function called to initialize the test environment.
func beforeTestSuiteSetup(ctx context.Context) {
	if diskInstall {
		By("Creating Disk for e2e tests")
		diskSetup(ctx)
	}

	if lvmOperatorInstall {
		By("BeforeTestSuite: deploying LVM Operator")
		deployLVMWithOLM(ctx, lvmCatalogSourceImage, lvmSubscriptionChannel)
	}
}

func VerifyLVMSSetup(ctx context.Context, cluster *v1alpha1.LVMCluster) {
	GinkgoHelper()
	validateLVMCluster(ctx, cluster)
	validateCSIDriver(ctx)
	validateVGManager(ctx)
	validateLVMVolumeGroup(ctx)
	validateStorageClass(ctx)
	for _, dc := range cluster.Spec.Storage.DeviceClasses {
		if dc.ThinPoolConfig != nil {
			validateVolumeSnapshotClass(ctx)
		}
	}
}

func GetStorageClass(ctx context.Context, name types.NamespacedName) *storagev1.StorageClass {
	GinkgoHelper()
	By(fmt.Sprintf("retrieving the Storage Class %q", name))
	// Make sure the storage class was configured properly
	sc := storagev1.StorageClass{}
	Eventually(func(ctx SpecContext) error {
		return crClient.Get(ctx, name, &sc)
	}, timeout, interval).WithContext(ctx).Should(Succeed())
	return &sc
}

func CreateResource(ctx context.Context, obj client.Object) {
	GinkgoHelper()
	gvk, _ := apiutil.GVKForObject(obj, crClient.Scheme())
	var key string
	if obj.GetNamespace() == "" {
		key = obj.GetName()
	} else {
		key = client.ObjectKeyFromObject(obj).String()
	}
	By(fmt.Sprintf("Creating %s %q", gvk.Kind, key))
	Expect(crClient.Create(ctx, obj)).To(Succeed())
}

func DeleteResource(ctx context.Context, obj client.Object) {
	GinkgoHelper()
	gvk, _ := apiutil.GVKForObject(obj, crClient.Scheme())
	var key string
	if obj.GetNamespace() == "" {
		key = obj.GetName()
	} else {
		key = client.ObjectKeyFromObject(obj).String()
	}
	By(fmt.Sprintf("Deleting %s %q", gvk.Kind, key))
	err := crClient.Delete(ctx, obj)
	if k8serrors.IsNotFound(err) {
		return
	}
	if meta.IsNoMatchError(err) {
		GinkgoLogr.Info("deletion was requested for resource that is not supported on the cluster",
			"gvk", gvk, "obj", client.ObjectKeyFromObject(obj))
		return
	}
	Expect(err).ToNot(HaveOccurred())
	Eventually(func(ctx context.Context) error {
		return crClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)
	}, timeout, interval).WithContext(ctx).Should(Satisfy(k8serrors.IsNotFound))
}

func lvmNamespaceCleanup(ctx context.Context) {
	DeleteResource(ctx, &k8sv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}})
}

// afterTestSuiteCleanup is the function called to tear down the test environment.
func afterTestSuiteCleanup(ctx context.Context) {
	if lvmOperatorUninstall {
		By("AfterTestSuite: uninstalling LVM Operator")
		uninstallLVM(ctx, lvmCatalogSourceImage, lvmSubscriptionChannel)
	}

	if diskInstall {
		By("Cleaning up disk")
		diskTeardown(ctx)
	}
}

func GetDefaultTestLVMClusterTemplate() *v1alpha1.LVMCluster {
	lvmClusterRes := &v1alpha1.LVMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultTestLVMCluster,
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

// createNamespace creates a namespace in the cluster, ignoring if it already exists.
func createNamespace(ctx context.Context, namespace string) {
	label := make(map[string]string)
	// label required for monitoring this namespace
	label["openshift.io/cluster-monitoring"] = "true"
	label["pod-security.kubernetes.io/enforce"] = "privileged"
	label["security.openshift.io/scc.podSecurityLabelSync"] = "false"

	annotations := make(map[string]string)
	// annotation required for workload partitioning
	annotations["workload.openshift.io/allowed"] = "management"

	ns := &k8sv1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        namespace,
			Annotations: annotations,
			Labels:      label,
		},
	}
	CreateResource(ctx, ns)
}

// DeleteResources in concurrent rows with sequential elements in each row
// for 3 rows with 3 objects each there will be 3 goroutines running deletions for
// 3 elements each
func DeleteResources(objRows [][]client.Object) func(ctx SpecContext) {
	GinkgoHelper()
	return func(ctx SpecContext) {
		wg := sync.WaitGroup{}
		for _, objs := range objRows {
			wg.Add(1)
			go func(ctx context.Context, objs []client.Object) {
				defer wg.Done()
				defer GinkgoRecover()
				for _, obj := range objs {
					DeleteResource(ctx, obj)
				}
			}(ctx, objs)
		}
		wg.Wait()
	}
}
