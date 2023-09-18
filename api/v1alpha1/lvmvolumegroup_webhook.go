package v1alpha1

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var lvmvolumegrouplog = logf.Log.WithName("lvmvolumegroup-webhook")

var _ webhook.Validator = &LVMVolumeGroup{}

//+kubebuilder:webhook:path=/validate-lvm-topolvm-io-v1alpha1-lvmvolumegroup,mutating=false,failurePolicy=fail,sideEffects=None,groups=lvm.topolvm.io,resources=lvmvolumegroups,verbs=create;update,versions=v1alpha1,name=vlvmvolumegroup.kb.io,admissionReviewVersions=v1

func (in *LVMVolumeGroup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(in).
		Complete()
}

func (in *LVMVolumeGroup) ValidateCreate() (warnings admission.Warnings, err error) {
	lvmvolumegrouplog.Info("validate create", "name", in.Name)
	return in.ValidateUpdate(nil)
}

func (in *LVMVolumeGroup) ValidateUpdate(_ runtime.Object) (warnings admission.Warnings, err error) {
	lvmvolumegrouplog.Info("validate update", "name", in.Name)
	if err := in.validateDeviceSelector(); err != nil {
		return nil, fmt.Errorf(".Spec.DeviceSelector is invalid: %w", err)
	}
	return nil, nil
}

func (in *LVMVolumeGroup) validateDeviceSelector() error {
	if in.Spec.DeviceSelector == nil {
		return nil
	}

	selector := in.Spec.DeviceSelector

	uniquePaths := make(map[string]bool)
	duplicatePaths := make(map[string]bool)

	// Check for duplicate required paths
	for _, path := range selector.Paths {
		if _, exists := uniquePaths[path]; exists {
			duplicatePaths[path] = true
			continue
		}

		uniquePaths[path] = true
	}

	// Check for duplicate optional paths
	for _, path := range selector.OptionalPaths {
		if _, exists := uniquePaths[path]; exists {
			duplicatePaths[path] = true
			continue
		}

		uniquePaths[path] = true
	}

	// Report any duplicate paths
	if len(duplicatePaths) > 0 {
		keys := make([]string, 0, len(duplicatePaths))
		for k := range duplicatePaths {
			keys = append(keys, k)
		}

		return fmt.Errorf("duplicate device paths found: %v", keys)
	}

	return nil
}

func (in *LVMVolumeGroup) ValidateDelete() (warnings admission.Warnings, err error) {
	lvmvolumegrouplog.Info("validate delete", "name", in.Name)
	return []string{}, nil
}
