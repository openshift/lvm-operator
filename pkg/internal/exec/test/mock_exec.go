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

type MockExecutor struct {
	MockExecuteCommandWithOutput       func(command string, arg ...string) (string, error)
	MockExecuteCommandWithOutputAsHost func(command string, arg ...string) (string, error)
}

// ExecuteCommandWithOutput mocks ExecuteCommandWithOutput
func (e *MockExecutor) ExecuteCommandWithOutput(command string, arg ...string) (string, error) {
	if e.MockExecuteCommandWithOutput != nil {
		return e.MockExecuteCommandWithOutput(command, arg...)
	}

	return "", nil
}

// ExecuteCommandWithOutputAsHost mocks ExecuteCommandWithOutputAsHost
func (e *MockExecutor) ExecuteCommandWithOutputAsHost(command string, arg ...string) (string, error) {
	if e.MockExecuteCommandWithOutputAsHost != nil {
		return e.MockExecuteCommandWithOutputAsHost(command, arg...)
	}

	return "", nil
}
