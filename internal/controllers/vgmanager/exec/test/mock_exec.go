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

package test

import (
	"context"
	"errors"
	"io"
	"strings"

	vgmanagerexec "github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/exec"
)

type MockExecutor struct {
	MockExecuteCommandWithOutputAsHost func(ctx context.Context, command string, arg ...string) (io.ReadCloser, error)

	MockRunCommandAsHost            func(ctx context.Context, command string, arg ...string) error
	MockRunCommandAsHostInto        func(ctx context.Context, into any, command string, arg ...string) error
	MockCombinedOutputCommandAsHost func(ctx context.Context, command string, arg ...string) ([]byte, error)
}

var _ vgmanagerexec.Executor = &MockExecutor{}

// StartCommandWithOutputAsHost mocks StartCommandWithOutputAsHost
func (e *MockExecutor) StartCommandWithOutputAsHost(ctx context.Context, command string, arg ...string) (io.ReadCloser, error) {
	if e.MockExecuteCommandWithOutputAsHost != nil {
		return e.MockExecuteCommandWithOutputAsHost(ctx, command, arg...)
	}

	return io.NopCloser(strings.NewReader("")), errors.New("StartCommandWithOutputAsHost not mocked")
}

// RunCommandAsHost mocks RunCommandAsHost
func (e *MockExecutor) RunCommandAsHost(ctx context.Context, command string, arg ...string) error {
	if e.MockRunCommandAsHost != nil {
		return e.MockRunCommandAsHost(ctx, command, arg...)
	}

	return errors.New("RunCommandAsHost not mocked")
}

// RunCommandAsHostInto mocks RunCommandAsHostInto
func (e *MockExecutor) RunCommandAsHostInto(ctx context.Context, into any, command string, arg ...string) error {
	if e.MockRunCommandAsHostInto != nil {
		return e.MockRunCommandAsHostInto(ctx, into, command, arg...)
	}

	return errors.New("RunCommandAsHostInto not mocked")
}

func (e *MockExecutor) CombinedOutputCommandAsHost(ctx context.Context, command string, arg ...string) ([]byte, error) {
	if e.MockCombinedOutputCommandAsHost != nil {
		return e.MockCombinedOutputCommandAsHost(ctx, command, arg...)
	}

	return nil, errors.New("CombinedOutputCommandAsHost not mocked")
}

func (e *MockExecutor) WrapCommandWithNSenter(command string, arg ...string) (string, []string) {
	return (&vgmanagerexec.CommandExecutor{}).WrapCommandWithNSenter(command, arg...)
}
