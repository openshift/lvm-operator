//go:build exclude

package tools

import (
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
	_ "sigs.k8s.io/kustomize/kustomize/v5"
)

// This file keeps the bundle tools in the module graph.
