package main

import (
	"fmt"
	"os"

	"github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	e "github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	g "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	"github.com/spf13/cobra"

	// Import your tests package for side effects (registers tests)
	_ "github.com/openshift/lvm-operator/v4/test/integration/helloworld"
)

func main() {
	registry := e.NewRegistry()
	ext := e.NewExtension("openshift", "payload", "lvm-operator")

	ext.AddSuite(e.Suite{
		Name:    "openshift/lvm-operator/test/integration",
		Parents: []string{},
	})

	specs, err := g.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite()
	if err != nil {
		panic(fmt.Sprintf("couldn't build extension test specs from ginkgo: %+v", err))
	}
	ext.AddSpecs(specs)
	registry.Register(ext)

	root := &cobra.Command{
		Long: "LVM Test Suite (OTE Based)",
	}

	root.AddCommand(cmd.DefaultExtensionCommands(registry)...)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
