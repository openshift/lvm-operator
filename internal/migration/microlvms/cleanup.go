package microlvms

import (
	"context"
	"errors"
	"fmt"

	"github.com/openshift/lvm-operator/v4/internal/cluster"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	TopoLVMLegacyControllerName    = "topolvm-controller"
	TopoLVMLegacyNodeDaemonSetName = "topolvm-node"
)

type Cleanup struct {
	namespace string
	client    client.Client
}

func NewCleanup(client client.Client, namespace string) *Cleanup {
	return &Cleanup{
		namespace: namespace,
		client:    client,
	}
}

// RemovePreMicroLVMSComponents is a method of the `Cleanup` struct that performs cleanup tasks for the components
// that ran pre MicroLVMS, e.g. separate controllers or daemonsets.
func (c *Cleanup) RemovePreMicroLVMSComponents(ctx context.Context) error {
	objs := []client.Object{
		// integrated into lvms operator
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      TopoLVMLegacyControllerName,
				Namespace: c.namespace,
			},
		},
		// integrated into vgmanager
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      TopoLVMLegacyNodeDaemonSetName,
				Namespace: c.namespace,
			},
		},
		// replaced by Replace Deployment strategy without Lease
		&coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.LeaseName,
				Namespace: c.namespace,
			},
		},
	}

	results := make(chan error, len(objs))
	deleteImmediatelyIfExistsByIndex := func(i int) {
		results <- c.deleteImmediatelyIfExists(ctx, objs[i])
	}
	for i := range objs {
		go deleteImmediatelyIfExistsByIndex(i)
	}

	var errs []error
	for i := 0; i < len(objs); i++ {
		if err := <-results; err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to run pre 4.16 MicroLVMS cleanup: %w", errors.Join(errs...))
	}

	return nil
}

func (c *Cleanup) deleteImmediatelyIfExists(ctx context.Context, obj client.Object) error {
	gvk, _ := apiutil.GVKForObject(obj, c.client.Scheme())
	logger := log.FromContext(ctx).WithValues("gvk", gvk.String(),
		"name", obj.GetName(), "namespace", obj.GetNamespace())

	if err := c.client.Delete(ctx, obj, &client.DeleteOptions{
		GracePeriodSeconds: ptr.To(int64(0)),
	}); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.V(1).Info("not found, nothing to delete in cleanup.")
			return nil
		}
		return fmt.Errorf("cleanup delete failed: %w", err)
	}

	logger.Info("delete successful.")
	return nil
}
