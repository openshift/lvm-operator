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

package vgmanager

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	configv1 "github.com/openshift/api/config/v1"
	secv1 "github.com/openshift/api/security/v1"
	lsblkmocks "github.com/openshift/lvm-operator/pkg/lsblk/mocks"
	lvmmocks "github.com/openshift/lvm-operator/pkg/lvm/mocks"
	"github.com/openshift/lvm-operator/pkg/lvmd"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"

	lvmv1alpha1 "github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/pkg/filter"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	k8sClient        client.Client
	testEnv          *envtest.Environment
	ctx              context.Context
	cancel           context.CancelFunc
	testNodeSelector corev1.NodeSelector
	testVGReconciler *VGReconciler
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

const (
	testNamespaceName = "openshift-storage"
	testNodeName      = "test-node"
	testHostname      = "test-host.vgmanager.test.io"
	timeout           = time.Second * 10
	interval          = time.Millisecond * 250
)

var _ = BeforeSuite(func() {
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	logf.SetLogger(logger)

	ctx, cancel = context.WithCancel(context.Background())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "test", "e2e", "testdata")},
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

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).ToNot(HaveOccurred())

	// CreateVG the primary namespace to be used by some tests
	testNamespace := &corev1.Namespace{}
	testNamespace.SetName(testNamespaceName)
	Expect(k8sClient.Create(ctx, testNamespace)).Should(Succeed())

	testNode := &corev1.Node{}
	testNode.SetName(testNodeName)
	hostnameKey := "kubernetes.io/hostname"
	testNode.SetLabels(map[string]string{
		hostnameKey: testHostname,
	})
	testNodeSelector = corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
		MatchExpressions: []corev1.NodeSelectorRequirement{{
			Key:      hostnameKey,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{testHostname},
		}},
	}}}
	Expect(k8sClient.Create(ctx, testNode)).Should(Succeed())

	testVGReconciler = &VGReconciler{
		Client:        k8sManager.GetClient(),
		Scheme:        k8sManager.GetScheme(),
		EventRecorder: k8sManager.GetEventRecorderFor(ControllerName),
		Namespace:     testNamespaceName,
		NodeName:      testNodeName,
		Filters:       filter.DefaultFilters,
	}
	err = (testVGReconciler).SetupWithManager(k8sManager)
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

func setupMocks() (*lvmmocks.MockLVM, *lsblkmocks.MockLSBLK) {
	t := GinkgoT()
	t.Helper()

	mockLSBLK := &lsblkmocks.MockLSBLK{}
	mockLSBLK.Mock.Test(t)
	DeferCleanup(func() {
		mockLSBLK.AssertExpectations(t)
	})
	mockLVM := &lvmmocks.MockLVM{}
	mockLVM.Mock.Test(t)
	DeferCleanup(func() {
		mockLVM.AssertExpectations(t)
	})
	testLVMD := lvmd.NewFileConfigurator(filepath.Join(t.TempDir(), "lvmd.yaml"))

	testVGReconciler.LSBLK = mockLSBLK
	testVGReconciler.LVM = mockLVM
	testVGReconciler.LVMD = testLVMD
	DeferCleanup(func() {
		testVGReconciler.LVMD = nil
		testVGReconciler.LSBLK = nil
		testVGReconciler.LVM = nil
	})

	return mockLVM, mockLSBLK
}
