package removal_test

import (
	"context"
	"testing"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/internal/controllers/node/removal"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestNodeRemovalController_SetupWithManager(t *testing.T) {
	mgr, err := controllerruntime.NewManager(&rest.Config{}, controllerruntime.Options{})
	assert.NoError(t, err)
	fakeclient := fake.NewClientBuilder().Build()
	r := removal.NewReconciler(fakeclient)
	assert.NoError(t, r.SetupWithManager(mgr))
}

func TestNodeRemovalController_GetNodeForLVMVolumeGroupNodeStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusName string
		objs       []client.Object
		get        error
		expect     []reconcile.Request
	}{
		{
			name:       "test node not found",
			statusName: "test-node",
			get:        errors.NewNotFound(schema.GroupResource{Group: "", Resource: "nodes"}, "test-node"),
		},
		{
			name:       "test node not fetch error",
			statusName: "test-node",
			get:        assert.AnError,
		},
		{
			name:       "test node found",
			statusName: "test-node",
			objs: []client.Object{
				&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}},
			},
			expect: []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "test-node"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newScheme := runtime.NewScheme()
			assert.NoError(t, lvmv1alpha1.AddToScheme(newScheme))
			assert.NoError(t, v1.AddToScheme(newScheme))
			clnt := fake.NewClientBuilder().WithObjects(tt.objs...).
				WithScheme(newScheme).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if tt.get != nil {
						return tt.get
					}
					return client.Get(ctx, key, obj, opts...)
				},
			}).Build()
			r := removal.NewReconciler(clnt)
			requests := r.GetNodeForLVMVolumeGroupNodeStatus(context.Background(),
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{ObjectMeta: metav1.ObjectMeta{Name: tt.statusName}})
			assert.ElementsMatch(t, tt.expect, requests)
		})
	}
}

func TestNodeRemovalController_Reconcile(t *testing.T) {
	defaultRequest := controllerruntime.Request{NamespacedName: types.NamespacedName{
		Name: "test-node",
	}}

	tests := []struct {
		name      string
		req       controllerruntime.Request
		objs      []client.Object
		get       error
		update    error
		list      error
		delete    error
		assertErr assert.ErrorAssertionFunc
	}{
		{
			name:      "test node not found (deleted after triggering reconcile)",
			req:       defaultRequest,
			get:       errors.NewNotFound(schema.GroupResource{Group: "", Resource: "nodes"}, "test-node"),
			assertErr: assert.NoError,
		},
		{
			name:      "test node not fetch error",
			req:       defaultRequest,
			get:       assert.AnError,
			assertErr: assert.Error,
		},
		{
			name: "test node not deleting but needs finalizer update",
			req:  defaultRequest,
			objs: []client.Object{
				&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: defaultRequest.Name}},
			},
			assertErr: assert.NoError,
		},
		{
			name: "test node not deleting but needs finalizer update which fails",
			req:  defaultRequest,
			objs: []client.Object{
				&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: defaultRequest.Name}},
			},
			update:    assert.AnError,
			assertErr: assert.Error,
		},
		{
			name: "test node with finalizer gets deleted and status list fails",
			req:  defaultRequest,
			objs: []client.Object{
				&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: defaultRequest.Name,
					Finalizers:        []string{removal.CleanupFinalizer},
					DeletionTimestamp: ptr.To(metav1.Now()),
				}},
			},
			list:      assert.AnError,
			assertErr: assert.Error,
		},
		{
			name: "test node with finalizer gets deleted and status list returns no nodestatus",
			req:  defaultRequest,
			objs: []client.Object{
				&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: defaultRequest.Name,
					Finalizers:        []string{removal.CleanupFinalizer},
					DeletionTimestamp: ptr.To(metav1.Now()),
				}},
			},
			assertErr: assert.NoError,
		},
		{
			name: "test node with finalizer gets deleted and status list returns node status but delete fails",
			req:  defaultRequest,
			objs: []client.Object{
				&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: defaultRequest.Name,
					Finalizers:        []string{removal.CleanupFinalizer},
					DeletionTimestamp: ptr.To(metav1.Now()),
				}},
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{ObjectMeta: metav1.ObjectMeta{Name: defaultRequest.Name}},
			},
			delete:    assert.AnError,
			assertErr: assert.Error,
		},
		{
			name: "test node with finalizer gets deleted and status list returns node status but finalizer removal fails",
			req:  defaultRequest,
			objs: []client.Object{
				&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: defaultRequest.Name,
					Finalizers:        []string{removal.CleanupFinalizer},
					DeletionTimestamp: ptr.To(metav1.Now()),
				}},
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{ObjectMeta: metav1.ObjectMeta{Name: defaultRequest.Name}},
			},
			update:    assert.AnError,
			assertErr: assert.Error,
		},
		{
			name: "test node with finalizer gets deleted and status list returns node status which gets deleted",
			req:  defaultRequest,
			objs: []client.Object{
				&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: defaultRequest.Name,
					Finalizers:        []string{removal.CleanupFinalizer},
					DeletionTimestamp: ptr.To(metav1.Now()),
				}},
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{ObjectMeta: metav1.ObjectMeta{Name: defaultRequest.Name}},
			},
			assertErr: assert.NoError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newScheme := runtime.NewScheme()
			assert.NoError(t, lvmv1alpha1.AddToScheme(newScheme))
			assert.NoError(t, v1.AddToScheme(newScheme))
			clnt := fake.NewClientBuilder().WithObjects(tt.objs...).
				WithScheme(newScheme).
				WithIndex(&lvmv1alpha1.LVMVolumeGroupNodeStatus{}, "metadata.name", func(object client.Object) []string {
					return []string{object.GetName()}
				}).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if tt.get != nil {
							return tt.get
						}
						return client.Get(ctx, key, obj, opts...)
					}, Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
						if tt.update != nil {
							return tt.update
						}
						return client.Update(ctx, obj, opts...)
					}, Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						if tt.delete != nil {
							return tt.delete
						}
						return client.Delete(ctx, obj, opts...)
					}, List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						if tt.list != nil {
							return tt.list
						}
						return client.List(ctx, list, opts...)
					}}).
				Build()
			r := removal.NewReconciler(clnt)

			_, err := r.Reconcile(context.Background(), tt.req)
			if tt.assertErr == nil {
				assert.NoError(t, err)
			} else {
				tt.assertErr(t, err)
			}
		})
	}
}
