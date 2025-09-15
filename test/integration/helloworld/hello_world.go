package helloworld

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HelloWorldTest", func() {
	It("prints Hello World", func() {
		println("Hello, World!")
		Expect(true).To(BeTrue())
	})
})
