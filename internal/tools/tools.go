//go:build exclude

package tools

import (
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
	_ "sigs.k8s.io/kustomize/kustomize/v4"
)

// This file is used to ensure that the bundle tools are included in the module.
