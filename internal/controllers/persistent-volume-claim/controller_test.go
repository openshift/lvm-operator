package persistent_volume_claim_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	persistentvolumeclaim "github.com/openshift/lvm-operator/v4/internal/controllers/persistent-volume-claim"
	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPersistentVolumeClaimReconciler_SetupWithManager(t *testing.T) {
	mgr, err := controllerruntime.NewManager(&rest.Config{}, controllerruntime.Options{})
	assert.NoError(t, err)
	fakeclient := fake.NewClientBuilder().Build()
	r := persistentvolumeclaim.NewReconciler(fakeclient, record.NewFakeRecorder(1))
	assert.NoError(t, r.SetupWithManager(mgr))

	predicates := r.Predicates()
	assert.True(t, predicates.CreateFunc(event.CreateEvent{}))
	assert.True(t, predicates.UpdateFunc(event.UpdateEvent{}))
	assert.False(t, predicates.Delete(event.DeleteEvent{}))
	assert.False(t, predicates.Generic(event.GenericEvent{}))
}

func TestPersistentVolumeClaimReconciler_Reconcile(t *testing.T) {
	defaultNamespace := "openshift-storage"

	tests := []struct {
		name                     string
		req                      controllerruntime.Request
		objs                     []client.Object
		clientGetErr             error
		clientListErr            error
		wantErr                  bool
		expectNoStorageAvailable bool
		expectRequeue            bool
	}{
		{
			name: "test persistent volume claim not found (deleted after triggering reconcile)",
			req: controllerruntime.Request{NamespacedName: types.NamespacedName{
				Namespace: defaultNamespace,
				Name:      "test-pvc",
			}},
		},
		{
			name: "test persistent volume claim get error",
			req: controllerruntime.Request{NamespacedName: types.NamespacedName{
				Namespace: defaultNamespace,
				Name:      "test-pvc",
			}},
			clientGetErr: assert.AnError,
			wantErr:      true,
		},
		{
			name: "testing set deletionTimestamp",
			req: controllerruntime.Request{NamespacedName: types.NamespacedName{
				Namespace: defaultNamespace,
				Name:      "test-deletionTimestamp",
			}},
			objs: []client.Object{
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Namespace: defaultNamespace, Name: "test-deletionTimestamp",
						DeletionTimestamp: &metav1.Time{Time: time.Now()}, Finalizers: []string{"random-finalizer"}},
				},
			},
		},
		{
			name: "testing empty storageClassName",
			req: controllerruntime.Request{NamespacedName: types.NamespacedName{
				Namespace: defaultNamespace,
				Name:      "test-emptyStorageClassName",
			}},
			objs: []client.Object{
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Namespace: defaultNamespace, Name: "test-emptyStorageClassName"},
				},
			},
		},
		{
			name: "testing non-applicable storageClassName",
			req: controllerruntime.Request{NamespacedName: types.NamespacedName{
				Namespace: defaultNamespace,
				Name:      "test-nonApplicableStorageClassName",
			}},
			objs: []client.Object{
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Namespace: defaultNamespace, Name: "test-nonApplicableStorageClassName"},
					Spec: v1.PersistentVolumeClaimSpec{
						StorageClassName: ptr.To("blabla"),
					},
				},
			},
		},
		{
			name: "testing non-pending PVC is skipped",
			req: controllerruntime.Request{NamespacedName: types.NamespacedName{
				Namespace: defaultNamespace,
				Name:      "test-non-pending-PVC",
			}},
			objs: []client.Object{
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Namespace: defaultNamespace, Name: "test-non-pending-PVC"},
					Spec: v1.PersistentVolumeClaimSpec{
						StorageClassName: ptr.To(constants.StorageClassPrefix + "bla"),
					},
					Status: v1.PersistentVolumeClaimStatus{
						Phase: v1.ClaimBound,
					},
				},
			},
		},
		{
			name: "testing pending PVC node list fails",
			req: controllerruntime.Request{NamespacedName: types.NamespacedName{
				Namespace: defaultNamespace,
				Name:      "test-pending-PVC",
			}},
			objs: []client.Object{
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Namespace: defaultNamespace, Name: "test-pending-PVC"},
					Spec: v1.PersistentVolumeClaimSpec{
						StorageClassName: ptr.To(constants.StorageClassPrefix + "bla"),
					},
					Status: v1.PersistentVolumeClaimStatus{
						Phase: v1.ClaimPending,
					},
				},
			},
			clientListErr: assert.AnError,
			wantErr:       true,
		},
		{
			name: "testing pending PVC is processed",
			req: controllerruntime.Request{NamespacedName: types.NamespacedName{
				Namespace: defaultNamespace,
				Name:      "test-pending-PVC",
			}},
			objs: []client.Object{
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Namespace: defaultNamespace, Name: "test-pending-PVC"},
					Spec: v1.PersistentVolumeClaimSpec{
						StorageClassName: ptr.To(constants.StorageClassPrefix + "bla"),
					},
					Status: v1.PersistentVolumeClaimStatus{
						Phase: v1.ClaimPending,
					},
				},
			},
			expectNoStorageAvailable: true,
			expectRequeue:            true,
		},
		{
			name: "testing PVC requesting more storage than capacity in the node",
			req: controllerruntime.Request{NamespacedName: types.NamespacedName{
				Namespace: defaultNamespace,
				Name:      "test-pending-PVC",
			}},
			objs: []client.Object{
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Namespace: defaultNamespace, Name: "test-pending-PVC"},
					Spec: v1.PersistentVolumeClaimSpec{
						StorageClassName: ptr.To(constants.StorageClassPrefix + "bla"),
						Resources: v1.VolumeResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceStorage: *resource.NewQuantity(100, resource.DecimalSI),
							},
						},
					},
					Status: v1.PersistentVolumeClaimStatus{
						Phase: v1.ClaimPending,
					},
				},
				&v1.Node{
					ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{persistentvolumeclaim.CapacityAnnotation + "bla": "10"}},
				},
			},
			expectNoStorageAvailable: true,
			expectRequeue:            true,
		},
		{
			name: "testing PVC requesting less storage than capacity in the node",
			req: controllerruntime.Request{NamespacedName: types.NamespacedName{
				Namespace: defaultNamespace,
				Name:      "test-pending-PVC",
			}},
			objs: []client.Object{
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Namespace: defaultNamespace, Name: "test-pending-PVC"},
					Spec: v1.PersistentVolumeClaimSpec{
						StorageClassName: ptr.To(constants.StorageClassPrefix + "bla"),
						Resources: v1.VolumeResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceStorage: *resource.NewQuantity(10, resource.DecimalSI),
							},
						},
					},
					Status: v1.PersistentVolumeClaimStatus{
						Phase: v1.ClaimPending,
					},
				},
				&v1.Node{
					ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{persistentvolumeclaim.CapacityAnnotation + "bla": "100"}},
				},
			},
			expectNoStorageAvailable: false,
			expectRequeue:            true,
		},
		{
			name: "testing PVC requesting less storage than capacity in one node, having another node without annotation",
			req: controllerruntime.Request{NamespacedName: types.NamespacedName{
				Namespace: defaultNamespace,
				Name:      "test-pending-PVC",
			}},
			objs: []client.Object{
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Namespace: defaultNamespace, Name: "test-pending-PVC"},
					Spec: v1.PersistentVolumeClaimSpec{
						StorageClassName: ptr.To(constants.StorageClassPrefix + "bla"),
						Resources: v1.VolumeResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceStorage: *resource.NewQuantity(10, resource.DecimalSI),
							},
						},
					},
					Status: v1.PersistentVolumeClaimStatus{
						Phase: v1.ClaimPending,
					},
				},
				&v1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "annotated", Annotations: map[string]string{persistentvolumeclaim.CapacityAnnotation + "bla": "100"}},
				},
				&v1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "idle"},
				},
			},
			expectNoStorageAvailable: false,
			expectRequeue:            true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := record.NewFakeRecorder(1)
			r := persistentvolumeclaim.NewReconciler(
				fake.NewClientBuilder().WithObjects(tt.objs...).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if tt.clientGetErr != nil {
								return tt.clientGetErr
							}
							return client.Get(ctx, key, obj, opts...)
						}, List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
							if tt.clientListErr != nil {
								return tt.clientListErr
							}
							return client.List(ctx, list, opts...)
						}}).Build(),
				recorder,
			)
			got, err := r.Reconcile(context.Background(), tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.expectRequeue && !reflect.DeepEqual(got, controllerruntime.Result{}) {
				t.Errorf("Reconcile() got non default Result")
				return
			}
			if tt.expectRequeue && !reflect.DeepEqual(got, controllerruntime.Result{RequeueAfter: 15 * time.Second}) {
				t.Errorf("Reconcile() got an unexpected Result")
				return
			}

			select {
			case event := <-recorder.Events:
				if !strings.Contains(event, "NotEnoughCapacity") {
					t.Errorf("event was captured but it did not contain the reason NotEnoughCapacity")
					return
				}
				if !tt.expectNoStorageAvailable {
					t.Errorf("event was captured but was not expected")
					return
				}
			case <-time.After(100 * time.Millisecond):
				if tt.expectNoStorageAvailable {
					t.Errorf("wanted event that no storage is available but none was sent")
					return
				}
			}
		})
	}
}
