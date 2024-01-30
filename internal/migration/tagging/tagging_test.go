package tagging_test

import (
	"context"
	"testing"

	"github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvm"
	lvmmocks "github.com/openshift/lvm-operator/internal/controllers/vgmanager/lvm/mocks"
	"github.com/openshift/lvm-operator/internal/migration/tagging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAddTagToVGs(t *testing.T) {
	namespace := "test-namespace"
	nodeName := "test-node"
	hostnameLabelKey := "kubernetes.io/hostname"
	vgName := "vgtest1"

	testCases := []struct {
		name          string
		clientObjects []client.Object
		volumeGroups  []lvm.VolumeGroup
		addTagCount   int
	}{
		{
			name: "there is a matching CR in the same namespace, add a tag",
			clientObjects: []client.Object{
				&v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{
					Name:      vgName,
					Namespace: namespace,
				}},
			},
			volumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
			},
			addTagCount: 1,
		},
		{
			name: "there are two matching CRs in the same namespace, add tags",
			clientObjects: []client.Object{
				&v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{
					Name:      vgName,
					Namespace: namespace,
				}},
				&v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{
					Name:      "vgtest2",
					Namespace: namespace,
				}},
			},
			volumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
				{
					Name: "vgtest2",
				},
			},
			addTagCount: 2,
		},
		{
			name:          "there is no matching CR, do not add a tag",
			clientObjects: []client.Object{},
			volumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
			},
			addTagCount: 0,
		},
		{
			name: "there is a matching CR in a different namespace, do not add a tag",
			clientObjects: []client.Object{
				&v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{
					Name:      vgName,
					Namespace: "test-namespace-2",
				}},
			},
			volumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
			},
			addTagCount: 0,
		},
		{
			name: "there is a matching CR with a matching node selector, add a tag",
			clientObjects: []client.Object{
				&v1alpha1.LVMVolumeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      vgName,
						Namespace: namespace,
					},
					Spec: v1alpha1.LVMVolumeGroupSpec{
						NodeSelector: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      hostnameLabelKey,
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{nodeName},
										},
									},
								},
							},
						},
					},
				},
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, Labels: map[string]string{
					hostnameLabelKey: nodeName,
				}}},
			},
			volumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
			},
			addTagCount: 1,
		},
		{
			name: "there is a matching CR with a non-matching node selector, do not add a tag",
			clientObjects: []client.Object{
				&v1alpha1.LVMVolumeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      vgName,
						Namespace: namespace,
					},
					Spec: v1alpha1.LVMVolumeGroupSpec{
						NodeSelector: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      hostnameLabelKey,
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"other-node-name"},
										},
									},
								},
							},
						},
					},
				},
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, Labels: map[string]string{
					hostnameLabelKey: nodeName,
				}}},
			},
			volumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
			},
			addTagCount: 0,
		},
	}

	mockLVM := lvmmocks.NewMockLVM(t)

	scheme, err := v1alpha1.SchemeBuilder.Build()
	assert.NoError(t, err, "creating scheme")
	err = corev1.AddToScheme(scheme)
	assert.NoError(t, err, "adding corev1 to scheme")

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.clientObjects...).Build()
			if tc.addTagCount > 0 {
				mockLVM.EXPECT().AddTagToVG(mock.Anything).Return(nil).Times(tc.addTagCount)
			}
			mockLVM.EXPECT().ListVGs().Return(tc.volumeGroups, nil).Once()
			err := tagging.AddTagToVGs(context.Background(), c, mockLVM, nodeName, namespace)
			assert.NoError(t, err)
		})
	}
}
