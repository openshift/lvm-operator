package deploymanager

import (
	"context"

	v1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (t *DeployManager) GenerateLVMCluster() *v1alpha1.LVMCluster {
	lvmClusterRes := &v1alpha1.LVMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lvmcluster-sample",
			Namespace: InstallNamespace,
		},
		Spec: v1alpha1.LVMClusterSpec{
			Storage: v1alpha1.Storage{
				DeviceClasses: []v1alpha1.DeviceClass{
					{
						Name: "vg1",
						ThinPoolConfig: &v1alpha1.ThinPoolConfig{
							Name:               "mytp1",
							SizePercent:        50,
							OverprovisionRatio: 50,
						},
					},
				},
			},
		},
	}
	return lvmClusterRes
}

// Creates a sample CR.
func (t *DeployManager) StartLVMCluster() error {
	lvmClusterRes := t.GenerateLVMCluster()
	return t.crClient.Create(context.TODO(), lvmClusterRes)
}

// Deletes a sample CR.
func (t *DeployManager) DeleteLVMCluster() error {
	lvmClusterRes := t.GenerateLVMCluster()
	return t.crClient.Delete(context.TODO(), lvmClusterRes)
}
