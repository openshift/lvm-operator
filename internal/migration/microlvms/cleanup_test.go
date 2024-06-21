package microlvms

import (
	"context"
	"errors"
	"testing"

	"github.com/openshift/lvm-operator/v4/internal/cluster"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestRemovePreMicroLVMSComponents(t *testing.T) {
	tests := []struct {
		name    string
		exist   bool
		wantErr bool
	}{
		{
			name:    "objects dont exist anymore (post-migration)",
			exist:   false,
			wantErr: false,
		},
		{
			name:    "objects still exist (pre-migration)",
			exist:   true,
			wantErr: false,
		},
		{
			name:    "objects exist but delete fails",
			exist:   true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace := "openshift-storage"
			fakeClientBuilder := fake.NewClientBuilder().
				WithScheme(setUpScheme()).
				WithObjects(setUpObjs(tt.exist, namespace)...)
			if tt.wantErr {
				fakeClientBuilder.WithInterceptorFuncs(interceptor.Funcs{
					Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						return errors.New("delete failed")
					},
				})
			}
			cleanup := NewCleanup(fakeClientBuilder.Build(), namespace)
			if err := cleanup.RemovePreMicroLVMSComponents(context.Background()); (err != nil) != tt.wantErr {
				t.Errorf("RemovePreMicroLVMSComponents() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func setUpScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = appsv1.AddToScheme(scheme)
	_ = coordinationv1.AddToScheme(scheme)
	return scheme
}

func setUpObjs(exist bool, namespace string) []client.Object {
	if exist {
		return nil
	}
	return []client.Object{
		&appsv1.Deployment{
			ObjectMeta: v1.ObjectMeta{
				Name:      TopoLVMLegacyControllerName,
				Namespace: namespace,
			},
		},
		&appsv1.DaemonSet{
			ObjectMeta: v1.ObjectMeta{
				Name:      TopoLVMLegacyNodeDaemonSetName,
				Namespace: namespace,
			},
		},
		&coordinationv1.Lease{
			ObjectMeta: v1.ObjectMeta{
				Name:      cluster.LeaseName,
				Namespace: namespace,
			},
		},
	}
}
