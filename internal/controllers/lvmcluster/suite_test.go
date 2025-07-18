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
	"fmt"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	secv1 "github.com/openshift/api/security/v1"
	"github.com/openshift/lvm-operator/v4/internal/cluster"
	"github.com/openshift/lvm-operator/v4/internal/controllers/constants"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/logpassthrough"
	"github.com/openshift/lvm-operator/v4/internal/controllers/lvmcluster/selector"
	"github.com/openshift/lvm-operator/v4/internal/controllers/node/removal"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
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
	testLvmClusterNamespace = "openshift-lvm-storage"
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

	Expect((&csiNodeReconciler{
		Client: k8sManager.GetClient(),
	}).SetupWithManager(ctx, k8sManager)).To(Succeed())

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

// csiNodeReconciler reconciles Nodes to create or delete CSINode objects with a driver for the TopoLVM CSI driver
// based on LVMCluster existence which mocks the kubelet CSI registration behavior.
type csiNodeReconciler struct {
	client.Client
}

// SetupWithManager sets up the controller with the Manager.
func (r *csiNodeReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	handle := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []reconcile.Request {
		nodes := &corev1.NodeList{}
		Expect(mgr.GetClient().List(ctx, nodes)).To(Succeed())
		toReconcile := make([]reconcile.Request, 0, len(nodes.Items))
		for _, node := range nodes.Items {
			toReconcile = append(toReconcile, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&node)})
		}
		return toReconcile
	})
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Watches(&lvmv1alpha1.LVMCluster{}, handle)

	return builder.Complete(r)
}

func (r *csiNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	lvmClusterList := &lvmv1alpha1.LVMClusterList{}
	if err := r.List(context.TODO(), lvmClusterList, &client.ListOptions{}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list LVMCluster instances: %w", err)
	}
	if size := len(lvmClusterList.Items); size > 1 {
		return ctrl.Result{}, fmt.Errorf("there should be a single LVMCluster but multiple were found, %d clusters found", size)
	}
	// get lvmcluster
	var lvmCluster *lvmv1alpha1.LVMCluster
	if len(lvmClusterList.Items) > 0 {
		lvmCluster = &lvmClusterList.Items[0]
	}

	node := &corev1.Node{}
	if err := r.Get(ctx, req.NamespacedName, node); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get node: %w", err)
	}
	csiNode := &v1.CSINode{}
	csiNode.SetName(node.Name)

	nodes := &corev1.NodeList{}
	nodes.Items = append(nodes.Items, *node)
	if lvmCluster != nil {
		validNodes, err := selector.ValidNodes(lvmCluster, nodes)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to get valid nodes: %w", err)
		}
		nodes.Items = validNodes
	}

	for _, node := range nodes.Items {
		if node.DeletionTimestamp.IsZero() {
			_, err := ctrl.CreateOrUpdate(ctx, r.Client, csiNode, func() error {
				csiNode.Spec.Drivers = []v1.CSINodeDriver{}
				if lvmCluster != nil && lvmCluster.DeletionTimestamp.IsZero() {
					csiNode.Spec.Drivers = append(csiNode.Spec.Drivers, v1.CSINodeDriver{
						Name:   constants.TopolvmCSIDriverName,
						NodeID: node.Name,
					})
				}
				return nil
			})
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to create or update CSINode: %w", err)
			}
		} else {
			if err := r.Delete(ctx, csiNode); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to delete CSINode: %w", err)
			}
		}
	}

	return ctrl.Result{}, nil
}
