package cluster

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestNewMasterSNOCheck(t *testing.T) {
	tests := []struct {
		name          string
		nodes         []client.Object
		simulateError bool
		wantErr       bool
		shouldBeSNO   bool
	}{
		{
			name:        "SNO",
			nodes:       []client.Object{&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "master1", Labels: map[string]string{ControlPlaneIDLabel: ""}}}},
			shouldBeSNO: true,
		},
		{
			name:        "Not SNO for only one worker",
			nodes:       []client.Object{&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker"}}},
			shouldBeSNO: false,
		},
		{
			name:          "Error when listing nodes",
			nodes:         []client.Object{&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "master1", Labels: map[string]string{ControlPlaneIDLabel: ""}}}},
			wantErr:       true,
			simulateError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clnt := fake.NewClientBuilder().
				WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						if tt.simulateError {
							return fmt.Errorf("im random")
						}
						return client.List(ctx, list, opts...)
					},
				}).WithObjects(tt.nodes...).Build()
			isSNO, err := NewMasterSNOCheck(clnt).IsSNO(context.Background())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.shouldBeSNO, isSNO)
			}
		})
	}
}
