package deploymanager

import (
	"context"
	"time"

	v1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
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
	cluster := &v1alpha1.LVMCluster{}
	err := t.crClient.Delete(context.TODO(), lvmClusterRes)
	if err != nil {
		return err
	}

	timeout := 600 * time.Second
	interval := 10 * time.Second

	// Wait for LVMCluster to be deleted
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		err = t.crClient.Get(context.TODO(), types.NamespacedName{Name: lvmClusterRes.Name, Namespace: InstallNamespace}, cluster)
		if err != nil && errors.IsNotFound(err) {
			return true, nil
		}
		if err == nil {
			return false, nil
		}
		return true, nil
	})

	return err
}
