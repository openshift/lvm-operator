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
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	secv1 "github.com/openshift/api/security/v1"
	"github.com/openshift/lvm-operator/v4/internal/cluster"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/logpassthrough"
	"github.com/openshift/lvm-operator/v4/internal/controllers/node/removal"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	k8sClient client.Client
	testEnv   *envtest.Environment
	cancel    context.CancelFunc
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

const (
	testLvmClusterName      = "test-lvmcluster"
	testThinPoolName        = "test-thinPool"
	testLvmClusterNamespace = "openshift-storage"
	testDeviceClassName     = "test"
	testImageName           = "test"
	testNodeName            = "test-node"
)

var _ = BeforeSuite(func() {
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	logf.SetLogger(logger)

	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "..", "test", "e2e", "testdata"),
		},
		ErrorIfCRDPathMissing: true,
		CRDInstallOptions: envtest.CRDInstallOptions{
			CleanUpAfterUse: true,
		},
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = lvmv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = topolvmv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = snapapi.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = secv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = configv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	Expect(k8sClient.Create(ctx, &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec:       configv1.InfrastructureSpec{},
	})).To(Succeed())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	// Create the primary namespace to be used by some tests
	testNamespace := &corev1.Namespace{}
	testNamespace.Name = testLvmClusterNamespace
	Expect(k8sClient.Create(ctx, testNamespace)).Should(Succeed())

	clusterType, err := cluster.NewTypeResolver(k8sClient).GetType(ctx)
	Expect(err).ToNot(HaveOccurred())

	enableSnapshotting := true
	vsc := &snapapi.VolumeSnapshotClassList{}
	if err := k8sClient.List(ctx, vsc, &client.ListOptions{Limit: 1}); err != nil {
		// this is necessary in case the VolumeSnapshotClass CRDs are not registered in the Distro, e.g. for OpenShift Local
		if meta.IsNoMatchError(err) {
			logger.Info("VolumeSnapshotClasses do not exist on the cluster, ignoring")
			enableSnapshotting = false
		}
	}

	err = (&Reconciler{
		Client:                k8sManager.GetClient(),
		EventRecorder:         k8sManager.GetEventRecorderFor("LVMClusterReconciler"),
		EnableSnapshotting:    enableSnapshotting,
		ClusterType:           clusterType,
		Namespace:             testLvmClusterNamespace,
		ImageName:             testImageName,
		LogPassthroughOptions: logpassthrough.NewOptions(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = removal.NewReconciler(k8sManager.GetClient(), testLvmClusterNamespace).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	Expect(testEnv.Stop()).To(Succeed())
})
