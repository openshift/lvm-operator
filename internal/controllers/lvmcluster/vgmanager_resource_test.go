/*
Copyright © 2023 Red Hat, Inc.

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

package lvmcluster

import (
	"context"
	"testing"

	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/logpassthrough"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/resource"
	"gotest.tools/v3/assert"

	"github.com/go-logr/logr/testr"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	testNamespace = "default"
)

func newFakeReconciler(t *testing.T, objs ...client.Object) *Reconciler {
	scheme, err := lvmv1alpha1.SchemeBuilder.Build()
	assert.NilError(t, err, "creating scheme")

	err = corev1.AddToScheme(scheme)
	assert.NilError(t, err, "adding corev1 to scheme")

	err = appsv1.AddToScheme(scheme)
	assert.NilError(t, err, "adding appsv1 to scheme")

	err = storagev1.AddToScheme(scheme)
	assert.NilError(t, err, "adding storagev1 to scheme")

	err = snapapi.AddToScheme(scheme)
	assert.NilError(t, err, "adding snapshot api to scheme")

	return &Reconciler{
		Client:                fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(),
		Namespace:             "default",
		LogPassthroughOptions: logpassthrough.NewOptions(),
	}
}

func TestVGManagerEnsureCreated(t *testing.T) {
	testTable := []struct {
		desc                string
		lvmclusterSpec      lvmv1alpha1.LVMClusterSpec
		expectedTolerations []corev1.Toleration
		expectedAffinity    *corev1.Affinity
	}{
		{
			desc: "nil nodeSelector in any of the deviceClasses",
			lvmclusterSpec: lvmv1alpha1.LVMClusterSpec{
				Storage: lvmv1alpha1.Storage{
					DeviceClasses: []lvmv1alpha1.DeviceClass{
						{NodeSelector: nil},
						{},
					},
				},
			},
			expectedTolerations: []corev1.Toleration{},
			expectedAffinity:    nil,
		},
	}

	for _, testCase := range testTable {
		lvmcluster := &lvmv1alpha1.LVMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testcluster",
				Namespace: testNamespace,
			},
			Spec: testCase.lvmclusterSpec,
		}
		r := newFakeReconciler(t, lvmcluster)
		var unit = resource.VGManager()
		err := unit.EnsureCreated(r, log.IntoContext(context.Background(), testr.New(t)), lvmcluster)
		assert.NilError(t, err, "running EnsureCreated")
		ds := &appsv1.DaemonSet{}
		err = r.Get(context.TODO(), types.NamespacedName{Name: resource.VGManagerUnit, Namespace: testNamespace}, ds)
		assert.NilError(t, err, "fetching daemonset")
		if testCase.expectedAffinity == nil {
			assert.Equal(t, testCase.expectedAffinity, ds.Spec.Template.Spec.Affinity)
		}
	}
}
