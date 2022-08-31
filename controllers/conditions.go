/*
Copyright 2022.

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

package controllers

import (
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"

	lvmv1alpha1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
)

func setLVMClusterCRvalidCondition(conditions *[]conditionsv1.Condition,
	status corev1.ConditionStatus, reason string, message string) {

	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    lvmv1alpha1.ConditionLVMClusterValid,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}
