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
	_ "embed"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	ginkgotypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"

	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
)

const (
	PodImageForPVCTests      = "public.ecr.aws/docker/library/busybox:1.36"
	DevicePathForPVCTests    = "/dev/xda"
	MountPathForPVCTests     = "/test1"
	VolumeNameForPVCTests    = "vol1"
	ContainerNameForPVCTests = "pause"
)

var (
	PodCommandForPVCTests = []string{"sh", "-c", "tail -f /dev/null"}
)

type pvcType string

const (
	pvcTypeEphemeral pvcType = "ephemeral"
	pvcTypeStatic    pvcType = "static"
)

type pvcTestOptions struct {
	k8sv1.PersistentVolumeMode
	pvcType
	snapshot bool
}

func pvcTestThinProvisioning() {
	var cluster *v1alpha1.LVMCluster
	BeforeAll(func(ctx SpecContext) {
		cluster = GetDefaultTestLVMClusterTemplate()
		CreateResource(ctx, cluster)
		VerifyLVMSSetup(ctx, cluster)
		DeferCleanup(func(ctx SpecContext) {
			if CurrentSpecReport().State.Is(ginkgotypes.SpecStateFailureStates) {
				By("Test failed, skipping cluster cleanup")
				skipSuiteCleanup.Store(true)
				return
			}
			DeleteResource(ctx, cluster)
		})
	})

	volumeModes := []k8sv1.PersistentVolumeMode{
		k8sv1.PersistentVolumeBlock,
		k8sv1.PersistentVolumeFilesystem,
	}

	pvcTypes := []pvcType{
		pvcTypeEphemeral,
		pvcTypeStatic,
	}

	for _, pvMode := range volumeModes {
		Context(fmt.Sprintf("PersistentVolumeMode: %s", string(pvMode)), func() {
			for _, pvcType := range pvcTypes {
				Context(fmt.Sprintf("PVC Type: %s", pvcType), func() {
					pvcTestsForMode(pvcTestOptions{
						PersistentVolumeMode: pvMode,
						pvcType:              pvcType,
						// Thick provisioning does not support Snapshot
						snapshot: true,
					})
				})
			}
		})
	}
}

func pvcTestThickProvisioning() {
	var cluster *v1alpha1.LVMCluster
	BeforeAll(func(ctx SpecContext) {
		cluster = GetDefaultTestLVMClusterTemplate()

		// set ThinPoolConfig to nil
		for _, dc := range cluster.Spec.Storage.DeviceClasses {
			dc.ThinPoolConfig = nil
		}

		CreateResource(ctx, cluster)
		VerifyLVMSSetup(ctx, cluster)
		DeferCleanup(func(ctx SpecContext) {
			if CurrentSpecReport().State.Is(ginkgotypes.SpecStateFailureStates) {
				By("Test failed, skipping cluster cleanup")
				skipSuiteCleanup.Store(true)
				return
			}
			DeleteResource(ctx, cluster)
		})
	})

	volumeModes := []k8sv1.PersistentVolumeMode{
		k8sv1.PersistentVolumeBlock,
		k8sv1.PersistentVolumeFilesystem,
	}

	pvcTypes := []pvcType{
		pvcTypeEphemeral,
		pvcTypeStatic,
	}

	for _, pvMode := range volumeModes {
		Context(fmt.Sprintf("PersistentVolumeMode: %s", string(pvMode)), func() {
			for _, pvcType := range pvcTypes {
				Context(fmt.Sprintf("PVC Type: %s", pvcType), func() {
					pvcTestsForMode(pvcTestOptions{
						PersistentVolumeMode: pvMode,
						pvcType:              pvcType,
						// Thick provisioning does not support Snapshot
						snapshot: false,
					})
				})
			}
		})
	}
}

func pvcTestsForMode(options pvcTestOptions) {
	var contentMode ContentMode
	switch options.PersistentVolumeMode {
	case k8sv1.PersistentVolumeBlock:
		contentMode = ContentModeBlock
	case k8sv1.PersistentVolumeFilesystem:
		contentMode = ContentModeFile
	}

	var pod *k8sv1.Pod
	var pvc *k8sv1.PersistentVolumeClaim
	switch options.pvcType {
	case pvcTypeStatic:
		pvc = generatePVC(options.PersistentVolumeMode)
		pod = generatePodConsumingPVC(pvc)
	case pvcTypeEphemeral:
		pod = generatePodWithEphemeralVolume(options.PersistentVolumeMode)
		// recreates locally what will be created as an ephemeral volume
		pvc = &k8sv1.PersistentVolumeClaim{}
		pvc.SetName(fmt.Sprintf("%s-%s", pod.GetName(), pod.Spec.Volumes[0].Name))
		pvc.SetNamespace(pod.GetNamespace())
		pvc.Spec.VolumeMode = &options.PersistentVolumeMode
	}

	clonePVC := generatePVCCloneFromPVC(pvc)
	clonePod := generatePodConsumingPVC(clonePVC)

	snapshot := generateVolumeSnapshot(pvc, snapshotClass)
	snapshotPVC := generatePVCFromSnapshot(options.PersistentVolumeMode, snapshot)
	snapshotPod := generatePodConsumingPVC(snapshotPVC)

	AfterAll(DeleteResources([][]client.Object{
		{snapshotPod, snapshotPVC, snapshot},
		{clonePod, clonePVC},
		{pod, pvc},
	}))

	expectedData := "TESTDATA"
	It("PVC and Pod", func(ctx SpecContext) {
		if options.pvcType == pvcTypeStatic {
			CreateResource(ctx, pvc)
		}
		CreateResource(ctx, pod)
		validatePodIsRunning(ctx, client.ObjectKeyFromObject(pod))
		validatePVCIsBound(ctx, client.ObjectKeyFromObject(pvc))

		Expect(contentTester.WriteDataInPod(ctx, pod, expectedData, contentMode)).To(Succeed())
		validatePodData(ctx, pod, expectedData, contentMode)
	})

	It("Cloning", func(ctx SpecContext) {
		CreateResource(ctx, clonePVC)
		CreateResource(ctx, clonePod)

		validatePodIsRunning(ctx, client.ObjectKeyFromObject(clonePod))
		validatePVCIsBound(ctx, client.ObjectKeyFromObject(clonePVC))
		validatePodData(ctx, clonePod, expectedData, contentMode)
	})

	if options.snapshot {
		It("Snapshots", func(ctx SpecContext) {
			createVolumeSnapshotFromPVCOrSkipIfUnsupported(ctx, snapshot)
			validateSnapshotReadyToUse(ctx, client.ObjectKeyFromObject(snapshot))

			CreateResource(ctx, snapshotPVC)
			CreateResource(ctx, snapshotPod)

			validatePodIsRunning(ctx, client.ObjectKeyFromObject(snapshotPod))
			validatePVCIsBound(ctx, client.ObjectKeyFromObject(snapshotPVC))
			validatePodData(ctx, snapshotPod, expectedData, contentMode)
		})
	}

}

func generatePVCSpec(mode k8sv1.PersistentVolumeMode) k8sv1.PersistentVolumeClaimSpec {
	return k8sv1.PersistentVolumeClaimSpec{
		VolumeMode:  ptr.To(mode),
		AccessModes: []k8sv1.PersistentVolumeAccessMode{k8sv1.ReadWriteOnce},
		Resources: k8sv1.VolumeResourceRequirements{Requests: map[k8sv1.ResourceName]resource.Quantity{
			k8sv1.ResourceStorage: resource.MustParse("1Gi"),
		}},
		StorageClassName: ptr.To(storageClassName),
	}
}

func generatePVC(mode k8sv1.PersistentVolumeMode) *k8sv1.PersistentVolumeClaim {
	return &k8sv1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.ToLower(string(mode)),
			Namespace: testNamespace,
		},
		Spec: generatePVCSpec(mode),
	}
}

func generatePVCCloneFromPVC(pvc *k8sv1.PersistentVolumeClaim) *k8sv1.PersistentVolumeClaim {
	clone := generatePVC(*pvc.Spec.VolumeMode)
	gvk, _ := apiutil.GVKForObject(pvc, crClient.Scheme())
	clone.SetName(fmt.Sprintf("%s-clone", pvc.GetName()))
	clone.Spec.DataSource = &k8sv1.TypedLocalObjectReference{
		Kind: gvk.Kind,
		Name: pvc.GetName(),
	}
	return clone
}

func generatePVCFromSnapshot(mode k8sv1.PersistentVolumeMode, snapshot *snapapi.VolumeSnapshot) *k8sv1.PersistentVolumeClaim {
	pvc := &k8sv1.PersistentVolumeClaim{}
	pvc.SetName(snapshot.GetName())
	pvc.SetNamespace(snapshot.GetNamespace())
	pvc.Spec = generatePVCSpec(mode)
	gvk, _ := apiutil.GVKForObject(snapshot, crClient.Scheme())
	pvc.Spec.DataSource = &k8sv1.TypedLocalObjectReference{
		Kind:     gvk.Kind,
		APIGroup: ptr.To(gvk.Group),
		Name:     snapshot.GetName(),
	}
	return pvc
}

func generateContainer(mode k8sv1.PersistentVolumeMode) k8sv1.Container {
	container := k8sv1.Container{
		Name:    ContainerNameForPVCTests,
		Command: PodCommandForPVCTests,
		Image:   PodImageForPVCTests,
		SecurityContext: &k8sv1.SecurityContext{
			RunAsNonRoot: ptr.To(true),
			SeccompProfile: &k8sv1.SeccompProfile{
				Type: k8sv1.SeccompProfileTypeRuntimeDefault,
			},
			Capabilities: &k8sv1.Capabilities{Drop: []k8sv1.Capability{"ALL"}},
		},
	}
	switch mode {
	case k8sv1.PersistentVolumeBlock:
		container.VolumeDevices = []k8sv1.VolumeDevice{{Name: VolumeNameForPVCTests, DevicePath: DevicePathForPVCTests}}
	case k8sv1.PersistentVolumeFilesystem:
		container.VolumeMounts = []k8sv1.VolumeMount{{Name: VolumeNameForPVCTests, MountPath: MountPathForPVCTests}}
	}
	return container
}

func generatePodConsumingPVC(pvc *k8sv1.PersistentVolumeClaim) *k8sv1.Pod {
	return &k8sv1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-consumer", pvc.GetName()),
			Namespace: testNamespace,
		},
		Spec: k8sv1.PodSpec{
			TerminationGracePeriodSeconds: ptr.To(int64(1)),
			Containers:                    []k8sv1.Container{generateContainer(*pvc.Spec.VolumeMode)},
			Volumes: []k8sv1.Volume{{
				Name: VolumeNameForPVCTests,
				VolumeSource: k8sv1.VolumeSource{
					PersistentVolumeClaim: &k8sv1.PersistentVolumeClaimVolumeSource{ClaimName: pvc.GetName()},
				},
			}},
		},
	}
}

func generatePodWithEphemeralVolume(mode k8sv1.PersistentVolumeMode) *k8sv1.Pod {
	return &k8sv1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ephemeral", strings.ToLower(string(mode))),
			Namespace: testNamespace,
		},
		Spec: k8sv1.PodSpec{
			TerminationGracePeriodSeconds: ptr.To(int64(1)),
			Containers:                    []k8sv1.Container{generateContainer(mode)},
			Volumes: []k8sv1.Volume{{
				Name: VolumeNameForPVCTests,
				VolumeSource: k8sv1.VolumeSource{
					Ephemeral: &k8sv1.EphemeralVolumeSource{VolumeClaimTemplate: &k8sv1.PersistentVolumeClaimTemplate{
						Spec: generatePVCSpec(mode)},
					},
				},
			}},
		},
	}
}

func generateVolumeSnapshot(pvc *k8sv1.PersistentVolumeClaim, snapshotClassName string) *snapapi.VolumeSnapshot {
	return &snapapi.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-snapshot", pvc.GetName()),
			Namespace: testNamespace,
		},
		Spec: snapapi.VolumeSnapshotSpec{
			Source:                  snapapi.VolumeSnapshotSource{PersistentVolumeClaimName: ptr.To(pvc.GetName())},
			VolumeSnapshotClassName: ptr.To(snapshotClassName),
		},
	}
}

func createVolumeSnapshotFromPVCOrSkipIfUnsupported(ctx context.Context, snapshot *snapapi.VolumeSnapshot) {
	GinkgoHelper()
	By(fmt.Sprintf("Creating VolumeSnapshot %q", snapshot.GetName()))
	err := crClient.Create(ctx, snapshot)
	if meta.IsNoMatchError(err) {
		Skip("Skipping Testing of VolumeSnapshot Operations due to lack of volume snapshot support")
	}
	Expect(err).ToNot(HaveOccurred(), "PVC should be created successfully")
}
