package lvmcluster

import (
	"context"
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const (
	defaultAnnotation = "storageclass.kubernetes.io/is-default-class"
	operatorManager   = "lvms-operator"
	userManager       = "kubectl"
)

func TestStorageClassDefaultAnnotationSSAOwnership(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add StorageClass API to scheme: %v", err)
	}

	testEnv := &envtest.Environment{}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}
	t.Cleanup(func() {
		NewWithT(t).Expect(testEnv.Stop()).To(Succeed())
	})

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("failed to create Kubernetes client: %v", err)
	}

	t.Run("removes operator-owned annotation", func(t *testing.T) {
		g := NewWithT(t)
		storageClassName := "ssa-default-operator-owned"
		cleanupStorageClass(t, ctx, k8sClient, storageClassName)

		applyStorageClass(t, ctx, k8sClient, storageClassName, true, operatorManager, true)
		applyStorageClass(t, ctx, k8sClient, storageClassName, false, operatorManager, true)

		storageClass := getStorageClass(t, ctx, k8sClient, storageClassName)
		_, hasAnnotation := storageClass.Annotations[defaultAnnotation]
		g.Expect(hasAnnotation).To(BeFalse())
	})

	t.Run("preserves user-owned annotation", func(t *testing.T) {
		g := NewWithT(t)
		storageClassName := "ssa-default-user-owned"
		cleanupStorageClass(t, ctx, k8sClient, storageClassName)

		applyStorageClass(t, ctx, k8sClient, storageClassName, false, operatorManager, true)
		applyStorageClass(t, ctx, k8sClient, storageClassName, true, userManager, false)
		applyStorageClass(t, ctx, k8sClient, storageClassName, false, operatorManager, true)

		storageClass := getStorageClass(t, ctx, k8sClient, storageClassName)
		g.Expect(storageClass.Annotations[defaultAnnotation]).To(Equal("true"))
	})

	t.Run("takes ownership from conflicting user value", func(t *testing.T) {
		g := NewWithT(t)
		storageClassName := "ssa-default-force-ownership"
		cleanupStorageClass(t, ctx, k8sClient, storageClassName)

		userStorageClass := newStorageClass(storageClassName, false)
		userStorageClass.Annotations[defaultAnnotation] = "false"
		g.Expect(k8sClient.Patch(ctx, userStorageClass, client.Apply, //nolint:staticcheck // SA1019: using deprecated client.Apply for SSA patch type
			client.FieldOwner(userManager))).To(Succeed())

		applyStorageClass(t, ctx, k8sClient, storageClassName, true, operatorManager, true)
		storageClass := getStorageClass(t, ctx, k8sClient, storageClassName)
		g.Expect(storageClass.Annotations[defaultAnnotation]).To(Equal("true"))
		g.Expect(managedBy(storageClass, operatorManager, defaultAnnotation)).To(BeTrue())
		g.Expect(managedBy(storageClass, userManager, defaultAnnotation)).To(BeFalse())

		applyStorageClass(t, ctx, k8sClient, storageClassName, false, operatorManager, true)
		storageClass = getStorageClass(t, ctx, k8sClient, storageClassName)
		_, hasAnnotation := storageClass.Annotations[defaultAnnotation]
		g.Expect(hasAnnotation).To(BeFalse())
	})
}

func applyStorageClass(t *testing.T, ctx context.Context, k8sClient client.Client, name string, isDefault bool, manager string, force bool) {
	t.Helper()
	patchOptions := []client.PatchOption{client.FieldOwner(manager)}
	if force {
		patchOptions = append(patchOptions, client.ForceOwnership)
	}
	NewWithT(t).Expect(k8sClient.Patch(ctx, newStorageClass(name, isDefault), client.Apply, //nolint:staticcheck // SA1019: using deprecated client.Apply for SSA patch type
		patchOptions...)).To(Succeed())
}

func getStorageClass(t *testing.T, ctx context.Context, k8sClient client.Client, name string) *storagev1.StorageClass {
	t.Helper()
	storageClass := &storagev1.StorageClass{}
	NewWithT(t).Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, storageClass)).To(Succeed())
	return storageClass
}

func cleanupStorageClass(t *testing.T, ctx context.Context, k8sClient client.Client, name string) {
	t.Helper()
	t.Cleanup(func() {
		storageClass := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: name}}
		err := k8sClient.Delete(ctx, storageClass)
		if err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("failed to delete StorageClass %q: %v", name, err)
		}
	})
}

func newStorageClass(name string, isDefault bool) *storagev1.StorageClass {
	annotations := map[string]string{}
	if isDefault {
		annotations[defaultAnnotation] = "true"
	}

	return &storagev1.StorageClass{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "storage.k8s.io/v1",
			Kind:       "StorageClass",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
		Provisioner: "test.csi",
	}
}

func managedBy(storageClass *storagev1.StorageClass, manager, annotation string) bool {
	for _, managedField := range storageClass.ManagedFields {
		if managedField.Manager != manager || managedField.FieldsV1 == nil {
			continue
		}

		fields := map[string]interface{}{}
		if err := json.Unmarshal(managedField.FieldsV1.GetRawBytes(), &fields); err != nil {
			continue
		}

		metadataFields, ok := fields["f:metadata"].(map[string]interface{})
		if !ok {
			continue
		}

		annotationFields, ok := metadataFields["f:annotations"].(map[string]interface{})
		if !ok {
			continue
		}

		if _, ownsAnnotation := annotationFields["f:"+annotation]; ownsAnnotation {
			return true
		}
	}

	return false
}
