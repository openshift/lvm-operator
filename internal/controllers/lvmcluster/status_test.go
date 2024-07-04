package lvmcluster

import (
	"context"
	"testing"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	vgProgressingCondition = metav1.Condition{
		Type:    lvmv1alpha1.VolumeGroupsReady,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonVGReadinessInProgress,
		Message: MessageVGReadinessInProgress,
	}
	vgDegradedCondition = metav1.Condition{
		Type:    lvmv1alpha1.VolumeGroupsReady,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonVGsDegraded,
		Message: MessageVGsDegraded,
	}
	vgFailedCondition = metav1.Condition{
		Type:    lvmv1alpha1.VolumeGroupsReady,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonVGsFailed,
		Message: MessageVGsFailed,
	}
	vgReadyCondition = metav1.Condition{
		Type:    lvmv1alpha1.VolumeGroupsReady,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonVGsReady,
		Message: MessageVGsReady,
	}
)

func TestIsNodeValid(t *testing.T) {
	testTable := []struct {
		desc              string
		nodeTaints        []corev1.Taint
		tolerations       []corev1.Toleration
		nodeSelector      *corev1.NodeSelector
		nodeLabels        map[string]string
		expectedNodeValid bool
	}{
		{
			desc:              "no taints, no tolerations, no nodeSelector, should return true",
			expectedNodeValid: true,
		},
		{
			desc: "no taints, no tolerations, nodeSelector matching the node, should return true",
			nodeSelector: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "kubernetes.io/hostname",
								Operator: "In",
								Values:   []string{"testNode"},
							},
						},
					},
				},
			},
			nodeLabels: map[string]string{
				"kubernetes.io/hostname": "testNode",
			},
			expectedNodeValid: true,
		},
		{
			desc: "no taints, no tolerations, nodeSelector not matching the node, should return false",
			nodeSelector: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "kubernetes.io/hostname",
								Operator: "In",
								Values:   []string{"testNode"},
							},
						},
					},
				},
			},
			nodeLabels: map[string]string{
				"kubernetes.io/hostname": "otherNode",
			},
			expectedNodeValid: false,
		},
		{
			desc: "taints without tolerations should return false",
			nodeTaints: []corev1.Taint{
				{
					Key:   "testTaint",
					Value: "testTaintVal",
				},
			},
			expectedNodeValid: false,
		},
		{
			desc: "taints with matching tolerations should return true",
			nodeTaints: []corev1.Taint{
				{
					Key:   "testTaint",
					Value: "testTaintVal",
				},
			},
			tolerations: []corev1.Toleration{
				{
					Key:   "testTaint",
					Value: "testTaintVal",
				},
			},
			expectedNodeValid: true,
		},
		{
			desc: "taints with not matching tolerations should return false",
			nodeTaints: []corev1.Taint{
				{
					Key:   "testTaint",
					Value: "testTaintVal",
				},
			},
			tolerations: []corev1.Toleration{
				{
					Key:   "otherTaint",
					Value: "otherTaintVal",
				},
			},
			expectedNodeValid: false,
		},
		{
			desc: "all set with matching values should return true",
			nodeTaints: []corev1.Taint{
				{
					Key:   "testTaint",
					Value: "testTaintVal",
				},
			},
			tolerations: []corev1.Toleration{
				{
					Key:   "testTaint",
					Value: "testTaintVal",
				},
			},
			nodeSelector: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "kubernetes.io/hostname",
								Operator: "In",
								Values:   []string{"testNode"},
							},
						},
					},
				},
			},
			nodeLabels: map[string]string{
				"kubernetes.io/hostname": "testNode",
			},
			expectedNodeValid: true,
		},
	}

	for _, testCase := range testTable {
		t.Run(testCase.desc, func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: testCase.nodeLabels,
				},
				Spec: corev1.NodeSpec{
					Taints: testCase.nodeTaints,
				},
			}

			deviceClass := &lvmv1alpha1.DeviceClass{
				NodeSelector: testCase.nodeSelector,
			}

			cluster := &lvmv1alpha1.LVMCluster{
				Spec: lvmv1alpha1.LVMClusterSpec{
					Storage: lvmv1alpha1.Storage{
						DeviceClasses: []lvmv1alpha1.DeviceClass{*deviceClass},
					},
					Tolerations: testCase.tolerations,
				},
			}

			nodeValid, err := isNodeValid(node, cluster, deviceClass)
			assert.NoError(t, err)
			assert.Equal(t, testCase.expectedNodeValid, nodeValid)
		})
	}
}

func TestSetVolumeGroupsReadyCondition(t *testing.T) {
	testTable := []struct {
		desc              string
		deviceClasses     []lvmv1alpha1.DeviceClass
		nodes             *corev1.NodeList
		vgNodeStatusList  *lvmv1alpha1.LVMVolumeGroupNodeStatusList
		expectedCondition metav1.Condition
	}{
		{
			desc: "ready vg should return ready condition",
			deviceClasses: []lvmv1alpha1.DeviceClass{
				{
					Name: "vg1",
				},
			},
			nodes: &corev1.NodeList{
				Items: []corev1.Node{
					{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				},
			},
			vgNodeStatusList: &lvmv1alpha1.LVMVolumeGroupNodeStatusList{
				Items: []lvmv1alpha1.LVMVolumeGroupNodeStatus{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node1",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusReady,
								},
							},
						},
					},
				},
			},
			expectedCondition: vgReadyCondition,
		},
		{
			desc: "no node status is found should return in progress condition",
			deviceClasses: []lvmv1alpha1.DeviceClass{
				{
					Name: "vg1",
				},
			},
			nodes: &corev1.NodeList{
				Items: []corev1.Node{
					{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				},
			},
			vgNodeStatusList:  &lvmv1alpha1.LVMVolumeGroupNodeStatusList{},
			expectedCondition: vgProgressingCondition,
		},
		{
			desc: "no VGStatus is found should return in progress condition",
			deviceClasses: []lvmv1alpha1.DeviceClass{
				{
					Name: "vg1",
				},
			},
			nodes: &corev1.NodeList{
				Items: []corev1.Node{
					{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				},
			},
			vgNodeStatusList: &lvmv1alpha1.LVMVolumeGroupNodeStatusList{
				Items: []lvmv1alpha1.LVMVolumeGroupNodeStatus{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node1",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg2",
									Status: lvmv1alpha1.VGStatusReady,
								},
							},
						},
					},
				},
			},
			expectedCondition: vgProgressingCondition,
		},
		{
			desc: "progressing vg should return progressing condition",
			deviceClasses: []lvmv1alpha1.DeviceClass{
				{
					Name: "vg1",
				},
			},
			nodes: &corev1.NodeList{
				Items: []corev1.Node{
					{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				},
			},
			vgNodeStatusList: &lvmv1alpha1.LVMVolumeGroupNodeStatusList{
				Items: []lvmv1alpha1.LVMVolumeGroupNodeStatus{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node1",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusProgressing,
								},
							},
						},
					},
				},
			},
			expectedCondition: vgProgressingCondition,
		},
		{
			desc: "degraded vg should return degraded condition",
			deviceClasses: []lvmv1alpha1.DeviceClass{
				{
					Name: "vg1",
				},
			},
			nodes: &corev1.NodeList{
				Items: []corev1.Node{
					{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				},
			},
			vgNodeStatusList: &lvmv1alpha1.LVMVolumeGroupNodeStatusList{
				Items: []lvmv1alpha1.LVMVolumeGroupNodeStatus{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node1",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusDegraded,
								},
							},
						},
					},
				},
			},
			expectedCondition: vgDegradedCondition,
		},
		{
			desc: "failed vg should return failed condition",
			deviceClasses: []lvmv1alpha1.DeviceClass{
				{
					Name: "vg1",
				},
			},
			nodes: &corev1.NodeList{
				Items: []corev1.Node{
					{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				},
			},
			vgNodeStatusList: &lvmv1alpha1.LVMVolumeGroupNodeStatusList{
				Items: []lvmv1alpha1.LVMVolumeGroupNodeStatus{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node1",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusFailed,
								},
							},
						},
					},
				},
			},
			expectedCondition: vgFailedCondition,
		},
		{
			desc: "failed&degraded vg should return failed condition",
			deviceClasses: []lvmv1alpha1.DeviceClass{
				{
					Name: "vg1",
				},
			},
			nodes: &corev1.NodeList{
				Items: []corev1.Node{
					{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				},
			},
			vgNodeStatusList: &lvmv1alpha1.LVMVolumeGroupNodeStatusList{
				Items: []lvmv1alpha1.LVMVolumeGroupNodeStatus{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node1",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusFailed,
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node2",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusDegraded,
								},
							},
						},
					},
				},
			},
			expectedCondition: vgFailedCondition,
		},
		{
			desc: "progressing&degraded vg should return degraded condition",
			deviceClasses: []lvmv1alpha1.DeviceClass{
				{
					Name: "vg1",
				},
			},
			nodes: &corev1.NodeList{
				Items: []corev1.Node{
					{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				},
			},
			vgNodeStatusList: &lvmv1alpha1.LVMVolumeGroupNodeStatusList{
				Items: []lvmv1alpha1.LVMVolumeGroupNodeStatus{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node1",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusProgressing,
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node2",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusDegraded,
								},
							},
						},
					},
				},
			},
			expectedCondition: vgDegradedCondition,
		},
		{
			desc: "progressing&ready vgs should return progressing condition",
			deviceClasses: []lvmv1alpha1.DeviceClass{
				{
					Name: "vg1",
				},
				{
					Name: "vg2",
				},
			},
			nodes: &corev1.NodeList{
				Items: []corev1.Node{
					{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				},
			},
			vgNodeStatusList: &lvmv1alpha1.LVMVolumeGroupNodeStatusList{
				Items: []lvmv1alpha1.LVMVolumeGroupNodeStatus{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node1",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusProgressing,
								},
								{
									Name:   "vg2",
									Status: lvmv1alpha1.VGStatusProgressing,
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node2",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusReady,
								},
								{
									Name:   "vg2",
									Status: lvmv1alpha1.VGStatusReady,
								},
							},
						},
					},
				},
			},
			expectedCondition: vgProgressingCondition,
		},
		{
			desc: "vg with nodeSelector not matching any node should return in progress condition",
			deviceClasses: []lvmv1alpha1.DeviceClass{
				{
					Name: "vg1",
					NodeSelector: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "kubernetes.io/hostname",
										Operator: "In",
										Values:   []string{"no-node"},
									},
								},
							},
						},
					},
				},
			},
			nodes: &corev1.NodeList{
				Items: []corev1.Node{
					{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				},
			},
			vgNodeStatusList:  &lvmv1alpha1.LVMVolumeGroupNodeStatusList{},
			expectedCondition: vgProgressingCondition,
		},
	}
	for _, testCase := range testTable {
		t.Run(testCase.desc, func(t *testing.T) {
			cluster := &lvmv1alpha1.LVMCluster{
				Spec: lvmv1alpha1.LVMClusterSpec{
					Storage: lvmv1alpha1.Storage{
						DeviceClasses: testCase.deviceClasses,
					},
				},
			}

			setVolumeGroupsReadyConditionInProgress(cluster)
			setVolumeGroupsReadyCondition(context.TODO(), cluster, testCase.nodes, testCase.vgNodeStatusList)
			exists := false
			for _, cond := range cluster.Status.Conditions {
				if cond.Type == testCase.expectedCondition.Type {
					exists = true
					assert.Equal(t, testCase.expectedCondition.Status, cond.Status)
					assert.Equal(t, testCase.expectedCondition.Reason, cond.Reason)
					assert.Equal(t, testCase.expectedCondition.Message, cond.Message)
				}
			}
			assert.Equal(t, true, exists)
		})
	}
}

func TestComputeDeviceClassStatuses(t *testing.T) {
	testTable := []struct {
		desc                        string
		vgNodeStatusList            *lvmv1alpha1.LVMVolumeGroupNodeStatusList
		expectedDeviceClassStatuses []lvmv1alpha1.DeviceClassStatus
	}{
		{
			desc: "all vgs are ready on all the nodes",
			vgNodeStatusList: &lvmv1alpha1.LVMVolumeGroupNodeStatusList{
				Items: []lvmv1alpha1.LVMVolumeGroupNodeStatus{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node1",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusReady,
								},
								{
									Name:   "vg2",
									Status: lvmv1alpha1.VGStatusReady,
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node2",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusReady,
								},
								{
									Name:   "vg2",
									Status: lvmv1alpha1.VGStatusReady,
								},
							},
						},
					},
				},
			},
			expectedDeviceClassStatuses: []lvmv1alpha1.DeviceClassStatus{
				{
					Name: "vg1",
					NodeStatus: []lvmv1alpha1.NodeStatus{
						{
							Node: "node1",
							VGStatus: lvmv1alpha1.VGStatus{
								Name:   "vg1",
								Status: lvmv1alpha1.VGStatusReady,
							},
						},
						{
							Node: "node2",
							VGStatus: lvmv1alpha1.VGStatus{
								Name:   "vg1",
								Status: lvmv1alpha1.VGStatusReady,
							},
						},
					},
				},
				{
					Name: "vg2",
					NodeStatus: []lvmv1alpha1.NodeStatus{
						{
							Node: "node1",
							VGStatus: lvmv1alpha1.VGStatus{
								Name:   "vg2",
								Status: lvmv1alpha1.VGStatusReady,
							},
						},
						{
							Node: "node2",
							VGStatus: lvmv1alpha1.VGStatus{
								Name:   "vg2",
								Status: lvmv1alpha1.VGStatusReady,
							},
						},
					},
				},
			},
		},
		{
			desc: "one vg failure on one node",
			vgNodeStatusList: &lvmv1alpha1.LVMVolumeGroupNodeStatusList{
				Items: []lvmv1alpha1.LVMVolumeGroupNodeStatus{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node1",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusFailed,
								},
								{
									Name:   "vg2",
									Status: lvmv1alpha1.VGStatusReady,
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node2",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusReady,
								},
								{
									Name:   "vg2",
									Status: lvmv1alpha1.VGStatusReady,
								},
							},
						},
					},
				},
			},
			expectedDeviceClassStatuses: []lvmv1alpha1.DeviceClassStatus{
				{
					Name: "vg1",
					NodeStatus: []lvmv1alpha1.NodeStatus{
						{
							Node: "node1",
							VGStatus: lvmv1alpha1.VGStatus{
								Name:   "vg1",
								Status: lvmv1alpha1.VGStatusFailed,
							},
						},
						{
							Node: "node2",
							VGStatus: lvmv1alpha1.VGStatus{
								Name:   "vg1",
								Status: lvmv1alpha1.VGStatusReady,
							},
						},
					},
				},
				{
					Name: "vg2",
					NodeStatus: []lvmv1alpha1.NodeStatus{
						{
							Node: "node1",
							VGStatus: lvmv1alpha1.VGStatus{
								Name:   "vg2",
								Status: lvmv1alpha1.VGStatusReady,
							},
						},
						{
							Node: "node2",
							VGStatus: lvmv1alpha1.VGStatus{
								Name:   "vg2",
								Status: lvmv1alpha1.VGStatusReady,
							},
						},
					},
				},
			},
		},
		{
			desc: "all vgs have different states",
			vgNodeStatusList: &lvmv1alpha1.LVMVolumeGroupNodeStatusList{
				Items: []lvmv1alpha1.LVMVolumeGroupNodeStatus{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node1",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusDegraded,
								},
								{
									Name:   "vg2",
									Status: lvmv1alpha1.VGStatusProgressing,
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node2",
						},
						Spec: lvmv1alpha1.LVMVolumeGroupNodeStatusSpec{
							LVMVGStatus: []lvmv1alpha1.VGStatus{
								{
									Name:   "vg1",
									Status: lvmv1alpha1.VGStatusReady,
								},
								{
									Name:   "vg2",
									Status: lvmv1alpha1.VGStatusFailed,
								},
							},
						},
					},
				},
			},
			expectedDeviceClassStatuses: []lvmv1alpha1.DeviceClassStatus{
				{
					Name: "vg1",
					NodeStatus: []lvmv1alpha1.NodeStatus{
						{
							Node: "node1",
							VGStatus: lvmv1alpha1.VGStatus{
								Name:   "vg1",
								Status: lvmv1alpha1.VGStatusDegraded,
							},
						},
						{
							Node: "node2",
							VGStatus: lvmv1alpha1.VGStatus{
								Name:   "vg1",
								Status: lvmv1alpha1.VGStatusReady,
							},
						},
					},
				},
				{
					Name: "vg2",
					NodeStatus: []lvmv1alpha1.NodeStatus{
						{
							Node: "node1",
							VGStatus: lvmv1alpha1.VGStatus{
								Name:   "vg2",
								Status: lvmv1alpha1.VGStatusProgressing,
							},
						},
						{
							Node: "node2",
							VGStatus: lvmv1alpha1.VGStatus{
								Name:   "vg2",
								Status: lvmv1alpha1.VGStatusFailed,
							},
						},
					},
				},
			},
		},
	}

	for _, testCase := range testTable {
		t.Run(testCase.desc, func(t *testing.T) {
			deviceClassStatuses := computeDeviceClassStatuses(testCase.vgNodeStatusList)
			assert.ElementsMatch(t, testCase.expectedDeviceClassStatuses, deviceClassStatuses)
		})
	}
}

func TestComputeReadiness(t *testing.T) {
	testTable := []struct {
		desc          string
		conditions    []metav1.Condition
		expectedState lvmv1alpha1.LVMStateType
		expectedReady bool
	}{
		{
			desc: "unknown state",
			conditions: []metav1.Condition{
				{
					Type:   lvmv1alpha1.ResourcesAvailable,
					Status: metav1.ConditionFalse,
					Reason: "UnknownReason",
				},
				{
					Type:   lvmv1alpha1.VolumeGroupsReady,
					Status: metav1.ConditionFalse,
					Reason: "UnknownReason",
				},
			},
			expectedState: lvmv1alpha1.LVMStatusUnknown,
			expectedReady: false,
		},
		{
			desc: "both progressing",
			conditions: []metav1.Condition{
				{
					Type:    lvmv1alpha1.ResourcesAvailable,
					Status:  metav1.ConditionFalse,
					Reason:  ReasonReconciliationInProgress,
					Message: MessageReconciliationInProgress,
				},
				{
					Type:    lvmv1alpha1.VolumeGroupsReady,
					Status:  metav1.ConditionFalse,
					Reason:  ReasonVGReadinessInProgress,
					Message: MessageVGReadinessInProgress,
				},
			},
			expectedState: lvmv1alpha1.LVMStatusProgressing,
			expectedReady: false,
		},
		{
			desc: "one progressing, one failing",
			conditions: []metav1.Condition{
				{
					Type:    lvmv1alpha1.ResourcesAvailable,
					Status:  metav1.ConditionFalse,
					Reason:  ReasonReconciliationInProgress,
					Message: MessageReconciliationInProgress,
				},
				{
					Type:    lvmv1alpha1.VolumeGroupsReady,
					Status:  metav1.ConditionFalse,
					Reason:  ReasonVGsFailed,
					Message: MessageVGsFailed,
				},
			},
			expectedState: lvmv1alpha1.LVMStatusFailed,
			expectedReady: false,
		},
		{
			desc: "one progressing, one degraded",
			conditions: []metav1.Condition{
				{
					Type:    lvmv1alpha1.ResourcesAvailable,
					Status:  metav1.ConditionFalse,
					Reason:  ReasonReconciliationInProgress,
					Message: MessageReconciliationInProgress,
				},
				{
					Type:    lvmv1alpha1.VolumeGroupsReady,
					Status:  metav1.ConditionFalse,
					Reason:  ReasonVGsDegraded,
					Message: MessageVGsDegraded,
				},
			},
			expectedState: lvmv1alpha1.LVMStatusDegraded,
			expectedReady: false,
		},
		{
			desc: "both failing",
			conditions: []metav1.Condition{
				{
					Type:    lvmv1alpha1.ResourcesAvailable,
					Status:  metav1.ConditionFalse,
					Reason:  ReasonResourcesSyncFailed,
					Message: MessageReasonResourcesSyncFailed,
				},
				{
					Type:    lvmv1alpha1.VolumeGroupsReady,
					Status:  metav1.ConditionFalse,
					Reason:  ReasonVGsFailed,
					Message: MessageVGsFailed,
				},
			},
			expectedState: lvmv1alpha1.LVMStatusFailed,
			expectedReady: false,
		},
		{
			desc: "one failing, one degraded",
			conditions: []metav1.Condition{
				{
					Type:    lvmv1alpha1.ResourcesAvailable,
					Status:  metav1.ConditionFalse,
					Reason:  ReasonResourcesSyncFailed,
					Message: MessageReasonResourcesSyncFailed,
				},
				{
					Type:    lvmv1alpha1.VolumeGroupsReady,
					Status:  metav1.ConditionFalse,
					Reason:  ReasonVGsDegraded,
					Message: MessageVGsDegraded,
				},
			},
			expectedState: lvmv1alpha1.LVMStatusFailed,
			expectedReady: false,
		},
		{
			desc: "both ready",
			conditions: []metav1.Condition{
				{
					Type:    lvmv1alpha1.ResourcesAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  ReasonResourcesAvailable,
					Message: MessageResourcesAvailable,
				},
				{
					Type:    lvmv1alpha1.VolumeGroupsReady,
					Status:  metav1.ConditionTrue,
					Reason:  ReasonVGsReady,
					Message: MessageVGsReady,
				},
			},
			expectedState: lvmv1alpha1.LVMStatusReady,
			expectedReady: true,
		},
		{
			desc: "unmanaged volume groups",
			conditions: []metav1.Condition{
				{
					Type:    lvmv1alpha1.ResourcesAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  ReasonResourcesAvailable,
					Message: MessageResourcesAvailable,
				},
				{
					Type:    lvmv1alpha1.VolumeGroupsReady,
					Status:  metav1.ConditionTrue,
					Reason:  ReasonVGsUnmanaged,
					Message: MessageVGsUnmanaged,
				},
			},
			expectedState: lvmv1alpha1.LVMStatusReady,
			expectedReady: true,
		},
		{
			desc: "unmanaged volume groups, but vgmanager is not ready",
			conditions: []metav1.Condition{
				{
					Type:    lvmv1alpha1.ResourcesAvailable,
					Status:  metav1.ConditionFalse,
					Reason:  ReasonResourcesSyncFailed,
					Message: MessageReasonResourcesSyncFailed,
				},
				{
					Type:    lvmv1alpha1.VolumeGroupsReady,
					Status:  metav1.ConditionTrue,
					Reason:  ReasonVGsUnmanaged,
					Message: MessageVGsUnmanaged,
				},
			},
			expectedState: lvmv1alpha1.LVMStatusFailed,
			expectedReady: false,
		},
	}
	for _, testCase := range testTable {
		t.Run(testCase.desc, func(t *testing.T) {
			state, ready := computeLVMClusterReadiness(testCase.conditions)
			assert.Equal(t, testCase.expectedState, state)
			assert.Equal(t, testCase.expectedReady, ready)
		})
	}
}
