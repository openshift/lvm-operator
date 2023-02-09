/*
Copyright 2022 Red Hat Openshift Data Foundation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"time"

	v1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
)

func generateLVMCluster() *v1alpha1.LVMCluster {
	lvmClusterRes := &v1alpha1.LVMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rh-lvmcluster",
			Namespace: installNamespace,
		},
		Spec: v1alpha1.LVMClusterSpec{
			Storage: v1alpha1.Storage{
				DeviceClasses: []v1alpha1.DeviceClass{
					{
						Name: "vg1",
						ThinPoolConfig: &v1alpha1.ThinPoolConfig{
							Name:               "mytp1",
							SizePercent:        90,
							OverprovisionRatio: 5,
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
