package v1alpha1

import (
	"errors"
	"fmt"
	"hash/fnv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/lvm-operator/v4/internal/cluster"

	corev1 "k8s.io/api/core/v1"
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
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: generatedName}}
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
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: generatedName}}
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
	},
		// It can happen that the creation of the LVMCluster is not yet visible to the webhook, so we retry a few times.
		// This is a workaround for the fact that the informer cache is not yet updated when the webhook is called.
		// This is faster / more efficient than waiting for the informer cache to be updated with a set interval.
		FlakeAttempts(3),
	)

	It("namespace cannot be looked up via ENV", func(ctx SpecContext) {
		generatedName := generateUniqueNameForTestCase(ctx)
		inacceptableNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: generatedName}}
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
		acceptableNamespace := "openshift-lvm-storage"
		GinkgoT().Setenv(cluster.OperatorNamespaceEnvVar, acceptableNamespace)
		generatedName := generateUniqueNameForTestCase(ctx)
		inacceptableNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: generatedName}}
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

	It("non-default device classes can be removed in update", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses = []DeviceClass{
			{
				Name: "test-device-class-1",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []DevicePath{
					"/dev/test1",
				}},
				FilesystemType: "xfs",
				Default:        true, // This is the default device class
			},
			{
				Name: "test-device-class-2",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []DevicePath{
					"/dev/test2",
				}},
				FilesystemType: "xfs",
				Default:        false, // This is a non-default device class
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		// Remove the non-default device class - this should succeed
		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses = []DeviceClass{
			{
				Name: "test-device-class-1",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []DevicePath{
					"/dev/test1",
				}},
				FilesystemType: "xfs",
				Default:        true,
			},
		}

		Expect(k8sClient.Update(ctx, updated)).To(Succeed())
		Expect(k8sClient.Delete(ctx, updated)).To(Succeed())
	})

	It("default device class cannot be removed in update", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses = []DeviceClass{
			{
				Name: "test-device-class-1",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []DevicePath{
					"/dev/test1",
				}},
				FilesystemType: "xfs",
				Default:        true, // This is the default device class
			},
			{
				Name: "test-device-class-2",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []DevicePath{
					"/dev/test2",
				}},
				FilesystemType: "xfs",
				Default:        false,
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		// Try to remove the default device class - this should fail
		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses = []DeviceClass{
			{
				Name: "test-device-class-2",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []DevicePath{
					"/dev/test2",
				}},
				FilesystemType: "xfs",
				Default:        false,
			},
		}

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring("cannot delete default device class"))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("all device classes cannot be removed in update", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses = []DeviceClass{
			{
				Name: "test-device-class-1",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []DevicePath{
					"/dev/test1",
				}},
				FilesystemType: "xfs",
				Default:        true,
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		// Try to remove all device classes - this should fail
		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses = []DeviceClass{}

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring("cannot remove all device classes"))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("multiple non-default device classes can be removed in single update", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses = []DeviceClass{
			{
				Name: "test-device-class-1",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []DevicePath{
					"/dev/test1",
				}},
				FilesystemType: "xfs",
				Default:        true, // This is the default device class
			},
			{
				Name: "test-device-class-2",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []DevicePath{
					"/dev/test2",
				}},
				FilesystemType: "xfs",
				Default:        false,
			},
			{
				Name: "test-device-class-3",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []DevicePath{
					"/dev/test3",
				}},
				FilesystemType: "xfs",
				Default:        false,
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		// Remove multiple non-default device classes at once - this should succeed
		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses = []DeviceClass{
			{
				Name: "test-device-class-1",
				ThinPoolConfig: &ThinPoolConfig{
					Name:               "thin-pool-1",
					SizePercent:        90,
					OverprovisionRatio: 10,
				},
				DeviceSelector: &DeviceSelector{Paths: []DevicePath{
					"/dev/test1",
				}},
				FilesystemType: "xfs",
				Default:        true,
			},
		}

		Expect(k8sClient.Update(ctx, updated)).To(Succeed())
		Expect(k8sClient.Delete(ctx, updated)).To(Succeed())
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
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{Paths: []DevicePath{
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
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{OptionalPaths: []DevicePath{
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
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{Paths: []DevicePath{
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
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{OptionalPaths: []DevicePath{
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

	It("updating NodeSelector is not allowed", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()

		updated.Spec.Storage.DeviceClasses[0].NodeSelector = &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{
			{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      "kubernetes.io/hostname",
						Operator: "In",
						Values:   []string{"some-node"},
					},
				},
			},
		}}

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrNodeSelectorCannotBeChanged.Error()))

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

	It("ThinPoolConfig.SizePercent of 100 is allowed but not recommended", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].ThinPoolConfig.SizePercent = 100

		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
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
		updated.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{Paths: []DevicePath{"/dev/newpath"}}

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
		updated.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{OptionalPaths: []DevicePath{"/dev/newpath"}}

		err := k8sClient.Update(ctx, updated)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrDevicePathsCannotBeAddedInUpdate.Error()))

		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("device paths can be removed but at least one device must remain", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{
			Paths:         []DevicePath{"/dev/path1", "/dev/path2"},
			OptionalPaths: []DevicePath{"/dev/optional1"},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		// Should succeed - removing some devices but keeping at least one
		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses[0].DeviceSelector.Paths = []DevicePath{"/dev/path1"}
		updated.Spec.Storage.DeviceClasses[0].DeviceSelector.OptionalPaths = []DevicePath{}
		Expect(k8sClient.Update(ctx, updated)).To(Succeed())

		// Should fail - removing all devices (start from the updated state)
		updated2 := updated.DeepCopy()
		updated2.Spec.Storage.DeviceClasses[0].DeviceSelector.Paths = []DevicePath{}
		updated2.Spec.Storage.DeviceClasses[0].DeviceSelector.OptionalPaths = []DevicePath{}
		err := k8sClient.Update(ctx, updated2)
		Expect(err).To(HaveOccurred())
		Expect(err).To(Satisfy(k8serrors.IsForbidden))
		statusError := &k8serrors.StatusError{}
		Expect(errors.As(err, &statusError)).To(BeTrue())
		Expect(statusError.Status().Message).To(ContainSubstring(ErrPathsOrOptionalPathsMandatoryWithNonNilDeviceSelector.Error()))

		Expect(k8sClient.Delete(ctx, updated2)).To(Succeed())
	})

	It("force wipe option cannot be added in update", func(ctx SpecContext) {
		resource := defaultLVMClusterInUniqueNamespace(ctx)
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{Paths: []DevicePath{"/dev/newpath"}}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses[0].DeviceSelector.ForceWipeDevicesAndDestroyAllData = ptr.To(true)

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
		resource.Spec.Storage.DeviceClasses[0].DeviceSelector = &DeviceSelector{Paths: []DevicePath{"/dev/newpath"}, ForceWipeDevicesAndDestroyAllData: ptr.To(false)}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		updated := resource.DeepCopy()
		updated.Spec.Storage.DeviceClasses[0].DeviceSelector.ForceWipeDevicesAndDestroyAllData = ptr.To(true)

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
