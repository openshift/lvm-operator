/*
Copyright 2022 Red Hat Openshift Data Foundation.

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
	label["pod-security.kubernetes.io/enforce"] = "privileged"
	label["security.openshift.io/scc.podSecurityLabelSync"] = "false"

	annotations := make(map[string]string)
	// annotation required for workload partitioning
	annotations["workload.openshift.io/allowed"] = "management"

	ns := &k8sv1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        namespace,
			Annotations: annotations,
			Labels:      label,
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
	label["pod-security.kubernetes.io/enforce"] = "baseline"
	label["security.openshift.io/scc.podSecurityLabelSync"] = "false"

	annotations := make(map[string]string)
	// annotation required for workload partitioning
	annotations["workload.openshift.io/allowed"] = "management"

	ns := &k8sv1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        namespace,
			Annotations: annotations,
			Labels:      label,
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
