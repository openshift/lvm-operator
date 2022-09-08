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

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var lvmclusterlog = logf.Log.WithName("lvmcluster-webhook")

var _ webhook.Validator = &LVMCluster{}

//+kubebuilder:webhook:path=/validate-lvm-topolvm-io-v1alpha1-lvmcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=lvm.topolvm.io,resources=lvmclusters,verbs=create;update,versions=v1alpha1,name=vlvmcluster.kb.io,admissionReviewVersions=v1

func (l *LVMCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(l).
		Complete()
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (l *LVMCluster) ValidateCreate() error {
	lvmclusterlog.Info("validate create", "name", l.Name)

	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (l *LVMCluster) ValidateUpdate(old runtime.Object) error {
	lvmclusterlog.Info("validate update", "name", l.Name)

	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (l *LVMCluster) ValidateDelete() error {
	lvmclusterlog.Info("validate delete", "name", l.Name)

	return nil
}
