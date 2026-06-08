module github.com/openshift/lvm-operator/v4/test/integration
go 1.23.9

toolchain go1.23.9
require (
	github.com/onsi/ginkgo/v2 v2.23.4
	github.com/onsi/gomega v1.37.0
	github.com/openshift-eng/openshift-tests-extension v0.0.0-20250916161632-d81c09058835
	github.com/openshift/lvm-operator/v4 v4.19.0
	github.com/spf13/cobra v1.9.1
	k8s.io/api v0.32.3
	k8s.io/apimachinery v0.32.3
	k8s.io/client-go v0.32.3
	k8s.io/klog/v2 v2.130.1
	sigs.k8s.io/controller-runtime v0.20.4
)
replace (
	github.com/onsi/ginkgo/v2 => github.com/openshift/onsi-ginkgo/v2 v2.6.1-0.20241205171354-8006f302fd12
)
