package cluster

import (
	"context"
	"fmt"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestNewTypeResolver(t *testing.T) {
	infra := &configv1.Infrastructure{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}}
	tests := []struct {
		name        string
		infra       *configv1.Infrastructure
		clientError error
		want        Type
		wantErr     bool
	}{
		{
			name:  "openshift infrastructure",
			infra: infra,
			want:  TypeOCP,
		},
		{
			name: "other infrastructure if no infra object is found",
			want: TypeOther,
		},
		{
			name:        "other infrastructure if infra CRD is not present in cluster",
			clientError: &meta.NoKindMatchError{},
			want:        TypeOther,
		},
		{
			name:        "error if unknown internal error occurred",
			clientError: fmt.Errorf("im random"),
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder()
			if tt.infra != nil {
				builder = builder.WithObjects(tt.infra)
			}
			builder.WithInterceptorFuncs(interceptor.Funcs{Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if tt.clientError != nil {
					return tt.clientError
				}
				return client.Get(ctx, key, obj, opts...)
			}})
			scheme := runtime.NewScheme()
			assert.NoError(t, configv1.Install(scheme))
			builder = builder.WithScheme(scheme)
			resolver := NewTypeResolver(builder.Build())

			typ, err := resolver.GetType(context.Background())
			if tt.wantErr {
				assert.Error(t, err)
				assert.ErrorContains(t, err, tt.clientError.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, typ)
			}
		})
	}
}
