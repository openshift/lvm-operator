package e2e

import (
	"context"
	"time"

	v1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
)

func generateLVMCluster() *v1alpha1.LVMCluster {
	lvmClusterRes := &v1alpha1.LVMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lvmcluster-sample",
			Namespace: installNamespace,
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

// startLVMCluster creates a sample CR.
func startLVMCluster(ctx context.Context) error {
	lvmClusterRes := generateLVMCluster()
	return crClient.Create(ctx, lvmClusterRes)
}

// deleteLVMCluster deletes a sample CR.
func deleteLVMCluster(ctx context.Context) error {
	lvmClusterRes := generateLVMCluster()
	cluster := &v1alpha1.LVMCluster{}
	err := crClient.Delete(ctx, lvmClusterRes)
	if err != nil {
		return err
	}

	timeout := 600 * time.Second
	interval := 10 * time.Second

	// wait for LVMCluster to be deleted
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		err = crClient.Get(ctx, types.NamespacedName{Name: lvmClusterRes.Name, Namespace: installNamespace}, cluster)
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
