/*
Copyright © 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"flag"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/go-logr/zapr"
	"sigs.k8s.io/controller-runtime/pkg/log"
	ctrlZap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestLvm(t *testing.T) {
	flag.Parse()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Lvm Suite")
}

var _ = BeforeSuite(func(ctx context.Context) {
	core := zapcore.NewCore(
		&ctrlZap.KubeAwareEncoder{Encoder: zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())},
		zapcore.AddSync(GinkgoWriter),
		zap.NewAtomicLevelAt(zapcore.Level(-9)),
	)
	zapLog := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	logr := zapr.NewLogger(zapLog.With(zap.Namespace("context")))
	log.SetLogger(logr)

	// Configure the disk and install the operator
	beforeTestSuiteSetup(ctx)
	createNamespace(ctx, testNamespace)
})

var _ = AfterSuite(func(ctx context.Context) {
	lvmNamespaceCleanup(ctx)
	afterTestSuiteCleanup(ctx)
})

var _ = Describe("LVM Operator e2e tests", func() {
	Describe("LVM Cluster Configuration", Serial, lvmClusterTest)
	Describe("PVC", Serial, Ordered, pvcTest)
})
