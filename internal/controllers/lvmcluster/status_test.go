package lvmcluster

import (
	"testing"

	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
