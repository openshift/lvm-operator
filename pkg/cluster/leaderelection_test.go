package cluster

import (
	"context"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/config/leaderelection"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
	"time"
)

func Test_nodeLookupSNOLeaderElection_Resolve(t *testing.T) {
	MultiNodeAssertion := func(a *assert.Assertions, le configv1.LeaderElection) bool {
		return a.Equal(137*time.Second, le.LeaseDuration.Duration) &&
			a.Equal(107*time.Second, le.RenewDeadline.Duration) &&
			a.Equal(26*time.Second, le.RetryPeriod.Duration)
	}

	SNOAssertion := func(a *assert.Assertions, le configv1.LeaderElection) bool {
		return a.Equal(270*time.Second, le.LeaseDuration.Duration) &&
			a.Equal(240*time.Second, le.RenewDeadline.Duration) &&
			a.Equal(60*time.Second, le.RetryPeriod.Duration)
	}

	tests := []struct {
		name      string
		nodes     []client.Object
		resolveFn func(a *assert.Assertions, le configv1.LeaderElection) bool
		errorFn   func(a *assert.Assertions, err error) bool
	}{
		{
			name: "LeaderElection Test Multi-Master",
			nodes: []client.Object{
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker1"}},
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker2"}},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "master1", Labels: map[string]string{ControlPlaneIDLabel: ""}},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "master2", Labels: map[string]string{ControlPlaneIDLabel: ""}},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "master3", Labels: map[string]string{ControlPlaneIDLabel: ""}},
				},
			},
			resolveFn: MultiNodeAssertion,
			errorFn: func(a *assert.Assertions, err error) bool {
				return a.NoError(err)
			},
		},
		{
			name: "LeaderElection Test SNO",
			nodes: []client.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "master1", Labels: map[string]string{ControlPlaneIDLabel: ""}},
				},
			},
			resolveFn: SNOAssertion,
			errorFn: func(a *assert.Assertions, err error) bool {
				return a.NoError(err)
			},
		},
		{
			name: "LeaderElection Test SNO (added workers)",
			nodes: []client.Object{
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker1"}},
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker2"}},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "master1", Labels: map[string]string{ControlPlaneIDLabel: ""}},
				},
			},
			resolveFn: SNOAssertion,
			errorFn: func(a *assert.Assertions, err error) bool {
				return a.NoError(err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clnt := fake.NewClientBuilder().WithObjects(tt.nodes...).Build()
			le := &nodeLookupSNOLeaderElection{
				clnt: clnt,
				defaultElectionConfig: leaderelection.LeaderElectionDefaulting(configv1.LeaderElection{},
					"test", "test-leader-id"),
			}
			got, err := le.Resolve(context.Background())
			assertions := assert.New(t)

			if !tt.errorFn(assertions, err) {
				return
			}
			if !tt.resolveFn(assertions, got) {
				return
			}
		})
	}
}
