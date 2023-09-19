package v1alpha1

import (
	"errors"
	"fmt"
	"hash/fnv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/lvm-operator/pkg/cluster"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func generateUniqueNameForTestCase(ctx SpecContext) string {
	GinkgoHelper()
	hash := fnv.New32()
	_, err := hash.Write([]byte(ctx.SpecReport().LeafNodeText))
	Expect(err).ToNot(HaveOccurred())
	name := fmt.Sprintf("test-%v", hash.Sum32())
	By(fmt.Sprintf("Test Case %q mapped to Unique Name %q", ctx.SpecReport().LeafNodeText, name))
	return name
}

var _ = Describe("webhook acceptance tests", func() {
	defaultClusterTemplate := &LVMCluster{
		Spec: LVMClusterSpec{
			Storage: Storage{
				DeviceClasses: []DeviceClass{{
					Name: "test-device-class",
					ThinPoolConfig: &ThinPoolConfig{
						Name:               "thin-pool-1",
						SizePercent:        90,
						OverprovisionRatio: 10,
					},
					Default:        true,
					FilesystemType: "xfs",
				}},
			},
		},
	}

	It("minimum viable configuration", func(ctx SpecContext) {
		generatedName := generateUniqueNameForTestCase(ctx)
		GinkgoT().Setenv(cluster.OperatorNamespaceEnvVar, generatedName)
		namespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: generatedName}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		})

		resource := defaultClusterTemplate.DeepCopy()
		resource.SetName(generatedName)
		resource.SetNamespace(namespace.GetName())

		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("duplicate LVMClusters get rejected", func(ctx SpecContext) {
		generatedName := generateUniqueNameForTestCase(ctx)
		GinkgoT().Setenv(cluster.OperatorNamespaceEnvVar, generatedName)
		namespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: generatedName}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		})

		resource := defaultClusterTemplate.DeepCopy()
		resource.SetName(generatedName)
		resource.SetNamespace(namespace.GetName())

		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		duplicate := resource.DeepCopy()
		duplicate.SetName(fmt.Sprintf("%s-dupe", duplicate.GetName()))

		err := k8sClient.Create(ctx, duplicate)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))

		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrDuplicateLVMCluster.Error()))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("namespace cannot be looked up via ENV", func(ctx SpecContext) {
		generatedName := generateUniqueNameForTestCase(ctx)
		inacceptableNamespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: generatedName}}
		Expect(k8sClient.Create(ctx, inacceptableNamespace)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			Expect(k8sClient.Delete(ctx, inacceptableNamespace)).To(Succeed())
		})

		resource := defaultClusterTemplate.DeepCopy()
		resource.SetName(generatedName)
		resource.SetNamespace(inacceptableNamespace.GetName())

		err := k8sClient.Create(ctx, resource)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))

		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(
			fmt.Sprintf("%s not found", cluster.OperatorNamespaceEnvVar)))
	})

	It("invalid namespace gets rejected", func(ctx SpecContext) {
		acceptableNamespace := "openshift-storage"
		GinkgoT().Setenv(cluster.OperatorNamespaceEnvVar, acceptableNamespace)
		generatedName := generateUniqueNameForTestCase(ctx)
		inacceptableNamespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: generatedName}}
		Expect(k8sClient.Create(ctx, inacceptableNamespace)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			Expect(k8sClient.Delete(ctx, inacceptableNamespace)).To(Succeed())
		})

		resource := defaultClusterTemplate.DeepCopy()
		resource.SetName(generatedName)
		resource.SetNamespace(inacceptableNamespace.GetName())

		err := k8sClient.Create(ctx, resource)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))

		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrInvalidNamespace.Error()))
	})

})
