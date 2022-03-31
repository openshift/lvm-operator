package e2e

import (
	"fmt"

	"github.com/onsi/ginkgo/v2"
)

//nolint:errcheck
func debug(msg string, args ...interface{}) {
	ginkgo.GinkgoWriter.Write([]byte(fmt.Sprintf(msg, args...)))
}
