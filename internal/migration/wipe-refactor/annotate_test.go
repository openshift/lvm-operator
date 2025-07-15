package wipe_refactor

import (
	"context"
	"testing"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const namespace = "openshift-lvm-storage"

func TestAnnotateExistingLVMVolumeGroupsIfWipingEnabled(t *testing.T) {
	truePtr := true
	tests := []struct {
		name                   string
		inObjs                 []client.Object
		expectedAnnotationKeys map[string][]string
	}{
		{
			name: "nodeStatus does not exist, return nil",
		},
		{
			name: "nodeStatus exist but wipe flag not set, return nil",
			inObjs: []client.Object{
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{
					Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
						LVMVGStatus: []lvmv1alpha1.VGStatus{
							{
								Name: "vg1",
							},
						},
					},
				},
				&lvmv1alpha1.LVMVolumeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vg1",
					},
				},
			},
		},
		{
			name: "nodeStatus exist and wipe flag set, annotate vg",
			inObjs: []client.Object{
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node1",
						Namespace: namespace,
					},
					Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
						LVMVGStatus: []lvmv1alpha1.VGStatus{
							{
								Name: "vg1",
							},
						},
					},
				},
				&lvmv1alpha1.LVMVolumeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vg1",
						Namespace: namespace,
					},
					Spec: lvmv1alpha1.LVMVolumeGroupSpec{
						DeviceSelector: &lvmv1alpha1.DeviceSelector{
							ForceWipeDevicesAndDestroyAllData: &truePtr,
						},
					},
				},
			},
			expectedAnnotationKeys: map[string][]string{"vg1": {constants.DevicesWipedAnnotationPrefix + "node1"}},
		},
		{
			name: "nodeStatus exist and wipe flag set for vg1, annotate vg1",
			inObjs: []client.Object{
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node1",
						Namespace: namespace,
					},
					Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
						LVMVGStatus: []lvmv1alpha1.VGStatus{
							{
								Name: "vg1",
							},
							{
								Name: "vg2",
							},
						},
					},
				},
				&lvmv1alpha1.LVMVolumeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vg1",
						Namespace: namespace,
					},
					Spec: lvmv1alpha1.LVMVolumeGroupSpec{
						DeviceSelector: &lvmv1alpha1.DeviceSelector{
							ForceWipeDevicesAndDestroyAllData: &truePtr,
						},
					},
				},
				&lvmv1alpha1.LVMVolumeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vg2",
						Namespace: namespace,
					},
				},
			},
			expectedAnnotationKeys: map[string][]string{"vg1": {constants.DevicesWipedAnnotationPrefix + "node1"}},
		},
		{
			name: "nodeStatus exist and wipe flag set for vg1 and vg2, annotate both",
			inObjs: []client.Object{
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node1",
						Namespace: namespace,
					},
					Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
						LVMVGStatus: []lvmv1alpha1.VGStatus{
							{
								Name: "vg1",
							},
							{
								Name: "vg2",
							},
						},
					},
				},
				&lvmv1alpha1.LVMVolumeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vg1",
						Namespace: namespace,
					},
					Spec: lvmv1alpha1.LVMVolumeGroupSpec{
						DeviceSelector: &lvmv1alpha1.DeviceSelector{
							ForceWipeDevicesAndDestroyAllData: &truePtr,
						},
					},
				},
				&lvmv1alpha1.LVMVolumeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vg2",
						Namespace: namespace,
					},
					Spec: lvmv1alpha1.LVMVolumeGroupSpec{
						DeviceSelector: &lvmv1alpha1.DeviceSelector{
							ForceWipeDevicesAndDestroyAllData: &truePtr,
						},
					},
				},
			},
			expectedAnnotationKeys: map[string][]string{"vg1": {constants.DevicesWipedAnnotationPrefix + "node1"}, "vg2": {constants.DevicesWipedAnnotationPrefix + "node1"}},
		},
		{
			name: "nodeStatus exist and wipe flag set, annotate vg on multiple nodes",
			inObjs: []client.Object{
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node1",
						Namespace: namespace,
					},
					Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
						LVMVGStatus: []lvmv1alpha1.VGStatus{
							{
								Name: "vg1",
							},
						},
					},
				},
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node2",
						Namespace: namespace,
					},
					Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
						LVMVGStatus: []lvmv1alpha1.VGStatus{
							{
								Name: "vg1",
							},
						},
					},
				},
				&lvmv1alpha1.LVMVolumeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vg1",
						Namespace: namespace,
					},
					Spec: lvmv1alpha1.LVMVolumeGroupSpec{
						DeviceSelector: &lvmv1alpha1.DeviceSelector{
							ForceWipeDevicesAndDestroyAllData: &truePtr,
						},
					},
				},
			},
			expectedAnnotationKeys: map[string][]string{"vg1": {constants.DevicesWipedAnnotationPrefix + "node1", constants.DevicesWipedAnnotationPrefix + "node2"}},
		},
		{
			name: "nodeStatus exist and wipe flag set for vg1 but not for vg2, annotate vg1 on multiple nodes",
			inObjs: []client.Object{
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node1",
						Namespace: namespace,
					},
					Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
						LVMVGStatus: []lvmv1alpha1.VGStatus{
							{
								Name: "vg1",
							},
							{
								Name: "vg2",
							},
						},
					},
				},
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node2",
						Namespace: namespace,
					},
					Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
						LVMVGStatus: []lvmv1alpha1.VGStatus{
							{
								Name: "vg1",
							},
							{
								Name: "vg2",
							},
						},
					},
				},
				&lvmv1alpha1.LVMVolumeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vg1",
						Namespace: namespace,
					},
					Spec: lvmv1alpha1.LVMVolumeGroupSpec{
						DeviceSelector: &lvmv1alpha1.DeviceSelector{
							ForceWipeDevicesAndDestroyAllData: &truePtr,
						},
					},
				},
				&lvmv1alpha1.LVMVolumeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vg2",
						Namespace: namespace,
					},
				},
			},
			expectedAnnotationKeys: map[string][]string{"vg1": {constants.DevicesWipedAnnotationPrefix + "node1", constants.DevicesWipedAnnotationPrefix + "node2"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(setUpScheme()).
				WithObjects(tt.inObjs...).
				Build()

			wipeRefactor := NewWipeRefactor(fakeClient, namespace)
			assert.NoError(t, wipeRefactor.AnnotateExistingLVMVolumeGroupsIfWipingEnabled(context.Background()))

			lvmVolumeGroups := &lvmv1alpha1.LVMVolumeGroupList{}
			assert.NoError(t, fakeClient.List(context.Background(), lvmVolumeGroups))

			for vgName, expectedAnnotationKeys := range tt.expectedAnnotationKeys {
				vgExists := false
				for _, volumeGroup := range lvmVolumeGroups.Items {
					if volumeGroup.Name == vgName {
						vgExists = true
						for _, key := range expectedAnnotationKeys {
							if _, ok := volumeGroup.Annotations[key]; !ok {
								t.Errorf("Annotation %s does not exist in LVMVolumeGroup %s", key, vgName)
							}
						}
					}
				}
				if !vgExists {
					t.Errorf("LVMVolumeGroup %s does not exist", vgName)
				}
			}
		})
	}
}

func setUpScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = lvmv1alpha1.AddToScheme(scheme)
	return scheme
}
