package e2e

import (
	"flag"
	"fmt"

	"github.com/onsi/ginkgo"
	deploymanager "github.com/red-hat-storage/lvm-operator/pkg/deploymanager"
)

// TestNamespace is the namespace we run all the tests in.
const TestNamespace = "lvm-endtoendtest"
const InstallNamespace = "openshift-storage"

var lvmOperatorInstall bool
var lvmOperatorUninstall bool

// LVMCatalogSourceImage is the LVM CatalogSource container image to use in the deployment
var LvmCatalogSourceImage string

// LvmSubscriptionChannel is the name of the lvm subscription channel
var LvmSubscriptionChannel string

// DiskInstall indicates whether disks are needed to be installed.
var DiskInstall bool

// DeployManager is the suite global DeployManager
var DeployManagerObj *deploymanager.DeployManager

// SuiteFailed indicates whether any test in the current suite has failed
var SuiteFailed = false

const StorageClass = "odf-lvm-vg1"

func init() {
	flag.StringVar(&LvmCatalogSourceImage, "lvm-catalog-image", "", "The LVM CatalogSource container image to use in the deployment")
	flag.StringVar(&LvmSubscriptionChannel, "lvm-subscription-channel", "", "The subscription channel to revise updates from")
	flag.BoolVar(&lvmOperatorInstall, "lvm-operator-install", true, "Install the LVM operator before starting tests")
	flag.BoolVar(&lvmOperatorUninstall, "lvm-operator-uninstall", true, "Uninstall the LVM cluster and operator after test completion")
	flag.BoolVar(&DiskInstall, "disk-install", false, "Create and attach disks to the nodes. This currently only works with AWS")

	d, err := deploymanager.NewDeployManager()
	if err != nil {
		panic(fmt.Sprintf("failed to initialize DeployManager: %v", err))
	}
	DeployManagerObj = d
}

//nolint:errcheck
func debug(msg string, args ...interface{}) {
	ginkgo.GinkgoWriter.Write([]byte(fmt.Sprintf(msg, args...)))
}
