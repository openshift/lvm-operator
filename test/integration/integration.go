package main

import (
	"fmt"
	"os"

	"github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	e "github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	g "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	"github.com/spf13/cobra"

	// Import the test packages
	_ "github.com/openshift/lvm-operator/v4/test/integration/sno"
)

func main() {
	registry := e.NewRegistry()
	ext := e.NewExtension("openshift", "payload", "lvm-operator")

	suites := []e.Suite{
		{
			Name: "openshift/lvm-operator/test/integration/single-node",
			Qualifiers: []string{
				`labels.exists(l, l=="SNO")`,
			},
		},
	}

	for _, suite := range suites {
		ext.AddSuite(suite)
	}

	specs, err := g.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite()
	if err != nil {
		panic(fmt.Sprintf("couldn't build extension test specs from ginkgo: %+v", err))
	}
	ext.AddSpecs(specs)
	registry.Register(ext)

	root := &cobra.Command{
		Long: "LVM Operator Test Suite (OTE Based)",
	}

	root.AddCommand(cmd.DefaultExtensionCommands(registry)...)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
