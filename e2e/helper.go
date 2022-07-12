package e2e

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/gomega"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
)

// createNamespace creates a namespace in the cluster, ignoring if it already exists.
func createNamespace(ctx context.Context, namespace string) error {
	label := make(map[string]string)
	// label required for monitoring this namespace
	label["openshift.io/cluster-monitoring"] = "true"
	ns := &k8sv1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: label,
		},
	}
	err := crClient.Create(ctx, ns)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// deleteNamespaceAndWait deletes a namespace and waits on it to terminate.
func deleteNamespaceAndWait(ctx context.Context, namespace string) error {
	label := make(map[string]string)
	// label required for monitoring this namespace
	label["openshift.io/cluster-monitoring"] = "true"
	ns := &k8sv1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: label,
		},
	}
	err := crClient.Delete(ctx, ns)
	gomega.Expect(err).To(gomega.BeNil())

	lastReason := ""
	timeout := 600 * time.Second
	interval := 10 * time.Second

	// wait for namespace to terminate
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		err = crClient.Get(ctx, types.NamespacedName{Name: namespace, Namespace: namespace}, ns)
		if err != nil && !errors.IsNotFound(err) {
			lastReason = fmt.Sprintf("Error talking to k8s apiserver: %v", err)
			return false, nil
		}
		if err == nil {
			lastReason = "Waiting on namespace to be deleted"
			return false, nil
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("%v: %s", err, lastReason)
	}
	return nil
}
