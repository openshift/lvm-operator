package removal_test

import (
	"context"
	"testing"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/internal/controllers/node/removal"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestNodeRemovalController_SetupWithManager(t *testing.T) {
	sch := runtime.NewScheme()
	assert.NoError(t, lvmv1alpha1.AddToScheme(sch))
	mgr, err := controllerruntime.NewManager(&rest.Config{}, controllerruntime.Options{Scheme: sch})
	assert.NoError(t, err)
	fakeclient := fake.NewClientBuilder().WithScheme(sch).Build()
	r := removal.NewReconciler(fakeclient, "test")
	assert.NoError(t, r.SetupWithManager(mgr))
}

func TestNodeRemovalController_GetNodeForLVMVolumeGroupNodeStatus(t *testing.T) {
	tests := []struct {
		name     string
		nodeName string
		objs     []client.Object
		get      error
		expect   []reconcile.Request
	}{
		{
			name:     "test node not found",
			nodeName: "test-node",
			get:      errors.NewNotFound(schema.GroupResource{Group: "", Resource: "LVMVolumeGroupNodeStatus"}, "test-node"),
		},
		{
			name:     "test node not fetch error",
			nodeName: "test-node",
			get:      assert.AnError,
		},
		{
			name:     "test node has status",
			nodeName: "test-node",
			objs: []client.Object{
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{ObjectMeta: metav1.ObjectMeta{Name: "test-node", Namespace: "test"}},
			},
			expect: []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "test-node", Namespace: "test"}}},
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
			r := removal.NewReconciler(clnt, "test")
			requests := r.GetNodeStatusFromNode(context.Background(),
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{ObjectMeta: metav1.ObjectMeta{Name: tt.nodeName}})
			assert.ElementsMatch(t, tt.expect, requests)
		})
	}
}

func TestNodeRemovalController_Reconcile(t *testing.T) {
	defaultRequest := controllerruntime.Request{NamespacedName: types.NamespacedName{
		Name:      "test-node",
		Namespace: "test",
	}}
	defaultMeta := metav1.ObjectMeta{Name: defaultRequest.Name, Namespace: defaultRequest.Namespace}

	tests := []struct {
		name                        string
		req                         controllerruntime.Request
		objs                        []client.Object
		getNode                     error
		getLVMVolumeGroupNodeStatus error
		delete                      error
		assertErr                   assert.ErrorAssertionFunc
	}{
		{
			name:                        "test node status not found (deleted after triggering reconcile)",
			req:                         defaultRequest,
			getLVMVolumeGroupNodeStatus: errors.NewNotFound(schema.GroupResource{Group: "", Resource: "nodes"}, "test-node"),
			assertErr:                   assert.NoError,
		},
		{
			name: "test node fetch error",
			req:  defaultRequest,
			objs: []client.Object{
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{ObjectMeta: defaultMeta},
			},
			getNode:   assert.AnError,
			assertErr: assert.Error,
		},
		{
			name: "test node gone but status still present so status deleted",
			req:  defaultRequest,
			objs: []client.Object{
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{ObjectMeta: defaultMeta},
			},
			assertErr: assert.NoError,
		},
		{
			name: "test node gone but status still present so status deleted but fails",
			req:  defaultRequest,
			objs: []client.Object{
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{ObjectMeta: defaultMeta},
			},
			delete:    assert.AnError,
			assertErr: assert.Error,
		},
		{
			name: "test node present and status present results in no-op",
			req:  defaultRequest,
			objs: []client.Object{
				&lvmv1alpha1.LVMVolumeGroupNodeStatus{ObjectMeta: defaultMeta},
				&v1.Node{ObjectMeta: defaultMeta},
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
						gvk, _ := apiutil.GVKForObject(obj, newScheme)
						switch gvk.Kind {
						case "Node":
							if tt.getNode != nil {
								return tt.getNode
							}
						case "LVMVolumeGroupNodeStatus":
							if tt.getLVMVolumeGroupNodeStatus != nil {
								return tt.getLVMVolumeGroupNodeStatus
							}
						}
						return client.Get(ctx, key, obj, opts...)
					}, Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						if tt.delete != nil {
							return tt.delete
						}
						return client.Delete(ctx, obj, opts...)
					}}).
				Build()
			r := removal.NewReconciler(clnt, defaultMeta.Namespace)

			_, err := r.Reconcile(context.Background(), tt.req)
			if tt.assertErr == nil {
				assert.NoError(t, err)
			} else {
				tt.assertErr(t, err)
			}
		})
	}
}
