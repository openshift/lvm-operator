package tagging_test

import (
	"context"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	lvmmocks "github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm/mocks"
	"github.com/openshift/lvm-operator/v4/internal/migration/tagging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"sigs.k8s.io/controller-runtime/pkg/log"

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

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, Labels: map[string]string{
		hostnameLabelKey: nodeName,
	}}}

	testCases := []struct {
		name                  string
		clientObjects         []client.Object
		untaggedVolumeGroups  []lvm.VolumeGroup
		listVGsErr            error
		addTagCountSuccessful int
		addTagCountError      int
	}{
		{
			name: "there is a matching CR in the same namespace, but no volume group to tag",
			clientObjects: []client.Object{
				&v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{
					Name:      vgName,
					Namespace: namespace,
				}},
			},
			untaggedVolumeGroups: []lvm.VolumeGroup{},
		},
		{
			name: "there is a matching CR in the same namespace, but vg fetching failed",
			clientObjects: []client.Object{
				&v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{
					Name:      vgName,
					Namespace: namespace,
				}},
			},
			listVGsErr: assert.AnError,
		},
		{
			name: "there is a matching CR in the same namespace, add a tag",
			clientObjects: []client.Object{
				&v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{
					Name:      vgName,
					Namespace: namespace,
				}},
				node,
			},
			untaggedVolumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
			},
			addTagCountSuccessful: 1,
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
				node,
			},
			untaggedVolumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
				{
					Name: "vgtest2",
				},
			},
			addTagCountSuccessful: 2,
		}, {
			name: "there are two matching CRs in the same namespace, add tags but fail on both of them",
			clientObjects: []client.Object{
				&v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{
					Name:      vgName,
					Namespace: namespace,
				}},
				&v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{
					Name:      "vgtest2",
					Namespace: namespace,
				}},
				node,
			},
			untaggedVolumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
				{
					Name: "vgtest2",
				},
			},
			addTagCountError: 2,
		},
		{
			name: "there are two matching CRs in the same namespace, add tags but fail on one of them",
			clientObjects: []client.Object{
				&v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{
					Name:      vgName,
					Namespace: namespace,
				}},
				&v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{
					Name:      "vgtest2",
					Namespace: namespace,
				}},
				node,
			},
			untaggedVolumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
				{
					Name: "vgtest2",
				},
			},
			addTagCountSuccessful: 1,
			addTagCountError:      1,
		},
		{
			name:          "there is no matching LVMVolumeGroup CR, do not add a tag",
			clientObjects: []client.Object{node},
			untaggedVolumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
			},
			addTagCountSuccessful: 0,
		},
		{
			name: "there is a matching CR in a different namespace, do not add a tag",
			clientObjects: []client.Object{
				&v1alpha1.LVMVolumeGroup{ObjectMeta: metav1.ObjectMeta{
					Name:      vgName,
					Namespace: "test-namespace-2",
				}},
				node,
			},
			untaggedVolumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
			},
			addTagCountSuccessful: 0,
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
				node,
			},
			untaggedVolumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
			},
			addTagCountSuccessful: 1,
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
				node,
			},
			untaggedVolumeGroups: []lvm.VolumeGroup{
				{
					Name: vgName,
				},
			},
			addTagCountSuccessful: 0,
		},
	}

	scheme, err := v1alpha1.SchemeBuilder.Build()
	assert.NoError(t, err, "creating scheme")
	err = corev1.AddToScheme(scheme)
	assert.NoError(t, err, "adding corev1 to scheme")

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			mockLVM := lvmmocks.NewMockLVM(t)
			defer mockLVM.AssertExpectations(t)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.clientObjects...).Build()
			if tc.addTagCountSuccessful > 0 {
				mockLVM.EXPECT().AddTagToVG(ctx, mock.Anything).Return(nil).Times(tc.addTagCountSuccessful)
			}
			if tc.addTagCountError > 0 {
				mockLVM.EXPECT().AddTagToVG(ctx, mock.Anything).Return(assert.AnError).Times(tc.addTagCountError)
			}

			if tc.listVGsErr != nil {
				mockLVM.EXPECT().ListVGs(ctx, false).Return(nil, tc.listVGsErr).Times(1)
			} else {
				mockLVM.EXPECT().ListVGs(ctx, false).Return(tc.untaggedVolumeGroups, nil).Times(1)
			}

			err := tagging.AddTagToVGs(ctx, c, mockLVM, nodeName, namespace)

			if tc.addTagCountError > 0 {
				assert.ErrorIs(t, err, assert.AnError)
			} else if tc.listVGsErr != nil {
				assert.ErrorIs(t, err, tc.listVGsErr)
			} else {
				assert.NoError(t, err)
			}

		})
	}
}
