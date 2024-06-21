package selector

import (
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"testing"
)

func TestExtractNodeSelectorAndTolerations(t *testing.T) {
	tests := []struct {
		name                  string
		lvmCluster            *lvmv1alpha1.LVMCluster
		expectedNodeKey       string
		expectedTolerationKey string
	}{
		{
			name: "NodeSelector and Toleration Extraction Test",
			lvmCluster: &lvmv1alpha1.LVMCluster{
				Spec: lvmv1alpha1.LVMClusterSpec{
					Storage: lvmv1alpha1.Storage{
						DeviceClasses: []lvmv1alpha1.DeviceClass{
							{
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "disktype",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{"ssd"},
												},
											},
										},
									},
								},
							},
						},
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "key1",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			expectedNodeKey:       "disktype",
			expectedTolerationKey: "key1",
		},
		// Add more test cases here
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeSelector, tolerations := ExtractNodeSelectorAndTolerations(tt.lvmCluster)

			assert.Equal(t, tt.expectedNodeKey, nodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key, "NodeSelector key does not match expected value")
			assert.Equal(t, tt.expectedTolerationKey, tolerations[0].Key, "Toleration key does not match expected value")
		})
	}
}
