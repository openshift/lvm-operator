package e2e

import (
	"github.com/openshift/lvm-operator/api/v1alpha1"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func raidTest() {
	Describe("RAID1", raid1Tests)
}

func raid1Tests() {
	raidConfig := &v1alpha1.RAIDConfig{
		Type:         v1alpha1.RAIDType1,
		MetadataSize: 512,
	}
	BeforeAll(startupLVMClusterWithRAIDConfig(raidConfig))

	Context("Validation of created resources", validateResources)
	Context("PersistentVolumeClaims, Snapshots & Cloning for Static Pods", pvcTest)
	Context("PersistentVolumeClaims, Snapshots & Cloning for Ephemeral Pods", ephemeralTest)
}

func startupLVMClusterWithRAIDConfig(raidConfig *v1alpha1.RAIDConfig) func(ctx SpecContext) {
	return func(ctx SpecContext) {
		clusterConfig := generateLVMCluster()
		clusterConfig.Spec.Storage.DeviceClasses[0].RAIDConfig = raidConfig
		lvmClusterSetup(clusterConfig, ctx)
		DeferCleanup(func(ctx SpecContext) {
			lvmClusterCleanup(clusterConfig, ctx)
		})
		By("Verifying that LVMCluster is ready")
		Eventually(clusterReadyCheck(clusterConfig), timeout, 300*time.Millisecond).WithContext(ctx).Should(Succeed())
	}
}
