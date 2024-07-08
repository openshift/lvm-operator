package persistent_volume_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	persistentvolume "github.com/openshift/lvm-operator/v4/internal/controllers/persistent-volume"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestPersistentVolumeReconciler_SetupWithManager(t *testing.T) {
	mgr, err := controllerruntime.NewManager(&rest.Config{}, controllerruntime.Options{})
	assert.NoError(t, err)
	fakeclient := fake.NewClientBuilder().Build()
	r := persistentvolume.NewReconciler(fakeclient, record.NewFakeRecorder(1))
	assert.NoError(t, r.SetupWithManager(mgr))

	predicates := r.Predicates()
	assert.True(t, predicates.CreateFunc(event.CreateEvent{}))
	assert.True(t, predicates.UpdateFunc(event.UpdateEvent{}))
	assert.False(t, predicates.Delete(event.DeleteEvent{}))
	assert.False(t, predicates.Generic(event.GenericEvent{}))
}

func TestPersistentVolumeReconciler_Reconcile(t *testing.T) {

	defaultRequest := controllerruntime.Request{NamespacedName: types.NamespacedName{
		Namespace: "openshift-storage",
		Name:      "test-pv",
	}}

	tests := []struct {
		name                      string
		req                       controllerruntime.Request
		objs                      []client.Object
		clientErr                 error
		assertErr                 assert.ErrorAssertionFunc
		checkClaimRefRemovedEvent bool
	}{
		{
			name: "test persistent volume not found (deleted after triggering reconcile)",
			req:  defaultRequest,
		},
		{
			name:      "test persistent volume fetch failed",
			req:       defaultRequest,
			clientErr: assert.AnError,
			assertErr: assert.Error,
		},
		{
			name: "persistent volume is present but has deletion timestamp",
			req:  defaultRequest,
			objs: []client.Object{
				&v1.PersistentVolume{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         defaultRequest.Namespace,
						Name:              defaultRequest.Name,
						DeletionTimestamp: ptr.To(metav1.Now()),
						Finalizers:        []string{"random-finalizer"},
					},
				},
			},
		},
		{
			name: "persistent volume is present but has mismatching prefix",
			req:  defaultRequest,
			objs: []client.Object{
				&v1.PersistentVolume{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: defaultRequest.Namespace,
						Name:      defaultRequest.Name,
					},
					Spec: v1.PersistentVolumeSpec{
						StorageClassName: "random-storage-class",
					},
				},
			},
		},
		{
			name: "persistent volume is present and has no claimRef",
			req:  defaultRequest,
			objs: []client.Object{
				&v1.PersistentVolume{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: defaultRequest.Namespace,
						Name:      defaultRequest.Name,
					},
					Spec: v1.PersistentVolumeSpec{
						StorageClassName: fmt.Sprintf("%s%s", constants.StorageClassPrefix, "example"),
					},
				},
			},
			checkClaimRefRemovedEvent: true,
		},
		{
			name: "persistent volume is present and has claimRef",
			req:  defaultRequest,
			objs: []client.Object{
				&v1.PersistentVolume{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: defaultRequest.Namespace,
						Name:      defaultRequest.Name,
					},
					Spec: v1.PersistentVolumeSpec{
						StorageClassName: fmt.Sprintf("%s%s", constants.StorageClassPrefix, "example"),
						ClaimRef: &v1.ObjectReference{
							Name: "blub",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := record.NewFakeRecorder(1)
			clnt := fake.NewClientBuilder().WithObjects(tt.objs...).
				WithInterceptorFuncs(interceptor.Funcs{Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if tt.clientErr != nil {
						return tt.clientErr
					}
					return client.Get(ctx, key, obj, opts...)
				}}).
				Build()
			r := persistentvolume.NewReconciler(clnt, recorder)

			_, err := r.Reconcile(context.Background(), tt.req)
			if tt.assertErr == nil {
				assert.NoError(t, err)
			} else {
				tt.assertErr(t, err)
			}

			if tt.checkClaimRefRemovedEvent {
				assert.NotEmpty(t, recorder.Events)
			} else {
				assert.Empty(t, recorder.Events)
			}
		})
	}
}
