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

package exec

import (
	"fmt"
	"os/exec"
	"strings"
)

var (
	nsenterPath = "/usr/bin/nsenter"
	losetupPath = "/usr/sbin/losetup"
)

// Executor is the  interface for running exec commands
type Executor interface {
	ExecuteCommandWithOutput(command string, arg ...string) (string, error)
	ExecuteCommandWithOutputAsHost(command string, arg ...string) (string, error)
}

// CommandExecutor is an Executor type
type CommandExecutor struct{}

// ExecuteCommandWithOutput executes a command with output
func (*CommandExecutor) ExecuteCommandWithOutput(command string, arg ...string) (string, error) {
	cmd := exec.Command(command, arg...)
	return runCommandWithOutput(cmd)
}

// ExecuteCommandWithOutput executes a command with output using nsenter
func (*CommandExecutor) ExecuteCommandWithOutputAsHost(command string, arg ...string) (string, error) {
	args := append([]string{"-m", "-u", "-i", "-n", "-p", "-t", "1", command}, arg...)
	cmd := exec.Command(nsenterPath, args...)
	return runCommandWithOutput(cmd)
}

func runCommandWithOutput(cmd *exec.Cmd) (string, error) {
	var output []byte
	var err error
	var out string

	output, err = cmd.Output()
	if err != nil {
		output = []byte(fmt.Sprintf("%s. %s", string(output), assertErrorType(err)))
	}

	out = strings.TrimSpace(string(output))

	if err != nil {
		return out, err
	}

	return out, nil
}

func assertErrorType(err error) string {
	switch errType := err.(type) {
	case *exec.ExitError:
		return string(errType.Stderr)
	case *exec.Error:
		return errType.Error()
	}

	return ""
}
