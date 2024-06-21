package v1alpha1

import (
	"errors"
	"fmt"
	"hash/fnv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/lvm-operator/v4/internal/cluster"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
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

	defaultLVMClusterInUniqueNamespace := func(ctx SpecContext) *LVMCluster {
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
		return resource
	}

	It("minimum viable create", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		Expect(resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig).ToNot(BeNil())
		Expect(resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSizeCalculationPolicy).
			To(Equal(ChunkSizeCalculationPolicyStatic))
		Expect(resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSize).To(BeNil())

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("minimum viable create for ReadWriteMany configuration", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig = nil
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
		duplicate.SetResourceVersion("")

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

	It("device classes cannot be removed in update", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses = []DeviceClass{
			{
				Name: "test-device-class-1",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []string{
					"/dev/test1",
				}},
				FilesystemType: "xfs",
			},
			{
				Name: "test-device-class-2",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []string{
					"/dev/test2",
				}},
				FilesystemType: "xfs",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses = []DeviceClass{
			{
				Name: "test-device-class-1",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []string{
					"/dev/test1",
				}},
				FilesystemType: "xfs",
			},
			{
				Name: "test-device-class-3",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []string{
					"/dev/test3",
				}},
				FilesystemType: "xfs",
			},
		}
		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring("device classes were deleted from the LVMCluster"))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("two default device classes are not allowed", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses = append(resource.Spec.Storage.DeviceClasses, DeviceClass{
			Name:           "dupe",
			Default:        true,
			ThinPoolConfig: resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.DeepCopy(),
		})

		err := k8sClient.Create(ctx, resource)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))

		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrOnlyOneDefaultDeviceClassAllowed.Error()))
	})

	It("no default device classes is allowed but outputs a warning", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].Default = false
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("device selector without path is invalid", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{}

		err := k8sClient.Create(ctx, resource)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))

		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrPathsOrOptionalPathsMandatoryWithNonNilDeviceSelector.Error()))
	})

	It("multiple device classes without path list are not allowed", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses = append(resource.Spec.Storage.DeviceClasses, DeviceClass{Name: "test",
			ThinPoolConfig: resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.DeepCopy()})

		err := k8sClient.Create(ctx, resource)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))

		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrEmptyPathsWithMultipleDeviceClasses.Error()))
	})

	It("device selector with non-dev path is forbidden", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{Paths: []string{
			"some-random-path",
		}}

		err := k8sClient.Create(ctx, resource)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))

		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring("must be an absolute path to the device"))
	})

	It("device selector with non-dev optional path is forbidden", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{OptionalPaths: []string{
			"some-random-path",
		}}

		err := k8sClient.Create(ctx, resource)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))

		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring("must be an absolute path to the device"))
	})

	It("device selector with overlapping devices in paths is forbidden", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{Paths: []string{
			"/dev/test1",
		}}
		dupe := *resource.Spec.Storage.DeviceClasses[0].DeepCopy()
		dupe.Name = "dupe"
		dupe.Default = false
		resource.Spec.Storage.DeviceClasses = append(resource.Spec.Storage.DeviceClasses, dupe)

		err := k8sClient.Create(ctx, resource)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))

		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring("overlaps in two different deviceClasss"))
	})

	It("device selector with overlapping devices in optional paths is forbidden", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{OptionalPaths: []string{
			"/dev/test1",
		}}
		dupe := *resource.Spec.Storage.DeviceClasses[0].DeepCopy()
		dupe.Name = "dupe"
		dupe.Default = false
		resource.Spec.Storage.DeviceClasses = append(resource.Spec.Storage.DeviceClasses, dupe)

		err := k8sClient.Create(ctx, resource)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))

		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring("overlaps in two different deviceClasss"))
	})

	It("minimum viable update", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()

		Expect(k8sClient.Update(ctx, updated)).To(Succeed())

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("updating ThinPoolConfig.Name is not allowed", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()

		updated.Spec.Storage.DeviceClasses[0].ThinPoolConfig.Name = "blub"

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrThinPoolConfigCannotBeChanged.Error()))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("updating ThinPoolConfig.SizePercent is not allowed", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()

		updated.Spec.Storage.DeviceClasses[0].ThinPoolConfig.SizePercent--

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrThinPoolConfigCannotBeChanged.Error()))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("updating ThinPoolConfig.OverprovisionRatio is not allowed", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()

		updated.Spec.Storage.DeviceClasses[0].ThinPoolConfig.OverprovisionRatio--

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrThinPoolConfigCannotBeChanged.Error()))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("updating ThinPoolConfig.ChunkSizeCalculationPolicy is not allowed", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()

		updated.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSizeCalculationPolicy = ChunkSizeCalculationPolicyHost

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrThinPoolConfigCannotBeChanged.Error()))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("updating ThinPoolConfig.ChunkSize is not allowed", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()

		updated.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSize = ptr.To(k8sresource.MustParse("500Ki"))

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrThinPoolConfigCannotBeChanged.Error()))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("device paths cannot be added to device class in update", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{Paths: []string{"/dev/newpath"}}

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrDevicePathsCannotBeAddedInUpdate.Error()))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("optional device paths cannot be added to device class in update", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{OptionalPaths: []string{"/dev/newpath"}}

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrDevicePathsCannotBeAddedInUpdate.Error()))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("device paths cannot be removed from device class in update", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{Paths: []string{"/dev/newpath"}}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses[0].DeviceSelector.Paths = []string{"/dev/otherpath"}

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring("required device paths were deleted from the LVMCluster"))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("optional device paths cannot be removed from device class in update", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{OptionalPaths: []string{"/dev/newpath"}}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses[0].DeviceSelector.OptionalPaths = []string{"/dev/otherpath"}

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring("optional device paths were deleted from the LVMCluster"))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("force wipe option cannot be added in update", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{Paths: []string{"/dev/newpath"}}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses[0].DeviceSelector.ForceWipeDevicesAndDestroyAllData = ptr.To[bool](true)

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrForceWipeOptionCannotBeChanged.Error()))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("force wipe option cannot be changed in update", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{Paths: []string{"/dev/newpath"}, ForceWipeDevicesAndDestroyAllData: ptr.To[bool](false)}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses[0].DeviceSelector.ForceWipeDevicesAndDestroyAllData = ptr.To[bool](true)

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrForceWipeOptionCannotBeChanged.Error()))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("chunk size change before create", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSize = ptr.To(k8sresource.MustParse("256Ki"))
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		Expect(resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig).ToNot(BeNil())
		Expect(resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSizeCalculationPolicy).
			To(Equal(ChunkSizeCalculationPolicyStatic))
		Expect(resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSize).ToNot(BeNil())
		Expect(resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSize.String()).To(Equal("256Ki"))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("host chunk policy with chunk size is forbidden", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSizeCalculationPolicy = ChunkSizeCalculationPolicyHost
		resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSize = ptr.To(k8sresource.MustParse("256Ki"))
		err := k8sClient.Create(ctx, resource)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
	})

	It("thin pool chunk size <64KiB is forbidden", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)

		resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSize = ptr.To(k8sresource.MustParse("32Ki"))

		err := k8sClient.Create(ctx, resource)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
	})

	It("thin pool chunk size >1Gi is forbidden", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)

		resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSize = ptr.To(k8sresource.MustParse("2Gi"))

		err := k8sClient.Create(ctx, resource)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
	})

	It("host chunk policy without chunk size is ok", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSizeCalculationPolicy = ChunkSizeCalculationPolicyHost
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		Expect(resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig).ToNot(BeNil())
		Expect(resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSizeCalculationPolicy).
			To(Equal(ChunkSizeCalculationPolicyHost))
		Expect(resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.ChunkSize).To(BeNil())

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

})
