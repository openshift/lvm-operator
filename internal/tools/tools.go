//go:build exclude

package tools

// Keep bundle build tools in go.mod so Konflux can prefetch their dependencies.
import (
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
	_ "sigs.k8s.io/kustomize/kustomize/v4"
)
