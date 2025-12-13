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
	"io"
	"os"
	"reflect"
	"time"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/resource"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/selector"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	k8sv1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
			GinkgoLogr.V(0).Info("Current LVM cluster devices are", "devices", currentCluster.Status.DeviceClassStatuses)
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

		return resource.VerifyDaemonSetReadiness(ds)
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

func validateCSINodeInfo(ctx context.Context, lvmCluster *v1alpha1.LVMCluster, shouldBePresent bool) bool {
	GinkgoHelper()
	By(fmt.Sprintf("validating the CSINode(s) for the Cluster to %shave the driver registered",
		map[bool]string{true: "", false: "NOT "}[shouldBePresent]))
	return Eventually(func(ctx context.Context) error {
		var nodes []k8sv1.Node

		nodeList := &k8sv1.NodeList{}
		if err := crClient.List(ctx, nodeList); err != nil {
			return fmt.Errorf("failed to list Nodes: %v", err)
		}

		cluster := lvmCluster
		if err := crClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster); errors.IsNotFound(err) {
			// LVMCluster not found to restrict search, validating all CSINodes in the Cluster
			nodes = nodeList.Items
		} else if err == nil {
			if nodes, err = selector.ValidNodes(cluster, nodeList); err != nil {
				return fmt.Errorf("failed to get valid nodes: %v", err)
			}
		} else {
			return fmt.Errorf("failed to get LVMCluster %q: %v", cluster.Name, err)
		}

		for _, node := range nodes {
			var csiNode storagev1.CSINode
			if err := crClient.Get(ctx, client.ObjectKey{Name: node.Name}, &csiNode); err != nil {
				return fmt.Errorf("failed to get CSINode %q: %v", node.Name, err)
			}
			exists := false
			for _, d := range csiNode.Spec.Drivers {
				if d.Name == csiDriverName {
					exists = true
					break
				}
			}
			if exists != shouldBePresent {
				return fmt.Errorf("CSINode %q should %scontain the driver %q",
					node.Name,
					map[bool]string{true: "", false: "NOT "}[shouldBePresent],
					csiDriverName,
				)
			}
		}

		return nil
	}, timeout, interval).WithContext(ctx).Should(Succeed())
}

func SummaryOnFailure(ctx context.Context) {
	if !CurrentSpecReport().Failed() {
		GinkgoLogr.Info("skipping test namespace summary due to successful test run")
		return
	} else {
		GinkgoLogr.Info("generating test namespace summary right after test failure")
	}

	var writer io.Writer
	if summaryFile != "" {
		GinkgoLogr.Info("writing test summary to file", "file", summaryFile)
		file, err := os.OpenFile(summaryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			GinkgoLogr.Error(err, "Failed to open file for test summary")
			return
		}
		defer func() {
			if err := file.Close(); err != nil {
				GinkgoLogr.Error(err, "Failed to close file for test summary")
			}
		}()
		writer = file
	} else {
		writer = GinkgoWriter
	}

	namespaces := []string{installNamespace, testNamespace}
	for _, ns := range namespaces {
		// list and encode all k8s objects in the test namespace
		listAndEncodeToWriter(
			ctx,
			writer,
			&client.ListOptions{Namespace: ns},
			&v1alpha1.LVMClusterList{},
			&v1alpha1.LVMVolumeGroupList{},
			&v1alpha1.LVMVolumeGroupNodeStatusList{},
			&k8sv1.PodList{},
			&k8sv1.PersistentVolumeClaimList{},
		)
	}
	listAndEncodeToWriter(ctx,
		writer,
		&client.ListOptions{},
		&storagev1.StorageClassList{},
		&snapapi.VolumeSnapshotList{},
		&snapapi.VolumeSnapshotClassList{},
		&k8sv1.PersistentVolumeList{},
		&topolvmv1.LogicalVolumeList{},
		&k8sv1.NodeList{},
		&storagev1.CSINodeList{},
	)
	listAndEncodeToWriter(ctx,
		writer,
		&client.ListOptions{FieldSelector: fields.AndSelectors(
			fields.OneTermEqualSelector("involvedObject.kind", "PersistentVolumeClaim"),
		), Namespace: testNamespace},
		&k8sv1.EventList{})
	listAndEncodeToWriter(ctx,
		writer,
		&client.ListOptions{FieldSelector: fields.AndSelectors(
			fields.OneTermEqualSelector("involvedObject.kind", "Pod"),
		), Namespace: testNamespace},
		&k8sv1.EventList{})
}

var (
	summaryEncoder = json.NewSerializerWithOptions(
		json.DefaultMetaFactory,
		scheme,
		scheme,
		json.SerializerOptions{
			Yaml:   true,
			Pretty: true,
			Strict: true,
		},
	)
)

// listAndEncodeToWriter lists the given client.ObjectList and encodes each item to the GinkgoWriter.
// This function is used to print the summary of the test namespace.
// The generic yaml/json encoder is not used because it does not handle the output as kubernetes would
// (e.g. it does not include the apiVersion and kind fields in the right formats).
func listAndEncodeToWriter(ctx context.Context, writer io.Writer, options *client.ListOptions, typs ...client.ObjectList) {
	for _, list := range typs {
		if err := crClient.List(ctx, list, options); err != nil {
			GinkgoLogr.Error(err, "Failed to list LVMClusters in test namespace")
		}
		objs, err := GenericGetItemsFromList(list)
		if err != nil {
			GinkgoLogr.Error(err, "Failed to get LVMClusters from list")
		}
		for _, item := range objs {
			if _, err := writer.Write([]byte("---\n")); err != nil {
				GinkgoLogr.Error(err, "Failed to write separator in test summary")
			}
			if err := summaryEncoder.Encode(item, writer); err != nil {
				GinkgoLogr.Error(err, "Failed to encode item in test summary")
			}
		}
	}
}

// GenericGetItemsFromList returns a list of client.Object from a client.ObjectList.
// This function uses reflection to get the Items field from the list as a slice of client.Object.
// That's because the client.ObjectList interface does not provide a method to get the items.
func GenericGetItemsFromList(list client.ObjectList) ([]client.Object, error) {
	// Use reflection to get the value of the list
	listValue := reflect.ValueOf(list)

	// Ensure that the list is a pointer to a struct
	if listValue.Kind() != reflect.Ptr || listValue.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("list should be a pointer to a struct")
	}

	// Dereference the pointer to get the struct value
	listValue = listValue.Elem()

	// Get the Items field by name
	itemsField := listValue.FieldByName("Items")
	if !itemsField.IsValid() {
		return nil, fmt.Errorf("no Items field found in the list")
	}

	// Ensure that the Items field is a slice
	if itemsField.Kind() != reflect.Slice {
		return nil, fmt.Errorf("Items field is not a slice")
	}

	// Convert each item in the Items slice to a client.Object
	var result []client.Object
	for i := 0; i < itemsField.Len(); i++ {
		item := itemsField.Index(i).Addr().Interface() // Get the address of the item to get a pointer
		if obj, ok := item.(client.Object); ok {
			obj.SetManagedFields(nil)
			result = append(result, obj)
		} else {
			return nil, fmt.Errorf("item does not implement client.Object")
		}
	}

	return result, nil
}

func validateDeviceRemovalSuccess(ctx context.Context, cluster *v1alpha1.LVMCluster, expectedDeviceCount int) bool {
	vgStatus := getVGStatusForCluster(ctx, cluster)
	return len(vgStatus.Devices) == expectedDeviceCount && vgStatus.Status == v1alpha1.VGStatusReady
}

func validateClusterReady(ctx context.Context, cluster *v1alpha1.LVMCluster) bool {
	vgStatus := getVGStatusForCluster(ctx, cluster)
	return vgStatus.Status == v1alpha1.VGStatusReady
}

func getVGStatusForCluster(ctx context.Context, cluster *v1alpha1.LVMCluster) v1alpha1.VGStatus {
	currentCluster := &v1alpha1.LVMCluster{}
	err := crClient.Get(ctx, client.ObjectKeyFromObject(cluster), currentCluster)
	if err != nil {
		return v1alpha1.VGStatus{}
	}

	if len(currentCluster.Status.DeviceClassStatuses) == 0 {
		return v1alpha1.VGStatus{}
	}

	// Find the first device class (usually "vg1")
	for _, deviceClassStatus := range currentCluster.Status.DeviceClassStatuses {
		if len(deviceClassStatus.NodeStatus) > 0 {
			// Return status from the first node with VG status
			for _, nodeStatus := range deviceClassStatus.NodeStatus {
				if nodeStatus.Name == lvmVolumeGroupName {
					return nodeStatus.VGStatus
				}
			}
		}
	}

	return v1alpha1.VGStatus{}
}
