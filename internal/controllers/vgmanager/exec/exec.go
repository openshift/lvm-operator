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
	"errors"
	"io"
	"os/exec"
	"strings"
)

var (
	nsenterPath = "/usr/bin/nsenter"
)

// Executor is the interface for running exec commands
type Executor interface {
	ExecuteCommandWithOutput(command string, arg ...string) (io.ReadCloser, error)
	ExecuteCommandWithOutputAsHost(command string, arg ...string) (io.ReadCloser, error)
}

// CommandExecutor is an Executor type
type CommandExecutor struct{}

// ExecuteCommandWithOutput executes a command with output
func (*CommandExecutor) ExecuteCommandWithOutput(command string, arg ...string) (io.ReadCloser, error) {
	cmd := exec.Command(command, arg...)
	return runCommandWithOutput(cmd)
}

// ExecuteCommandWithOutputAsHost executes a command with output using nsenter
func (*CommandExecutor) ExecuteCommandWithOutputAsHost(command string, arg ...string) (io.ReadCloser, error) {
	args := append([]string{"-m", "-u", "-i", "-n", "-p", "-t", "1", command}, arg...)
	cmd := exec.Command(nsenterPath, args...)
	return runCommandWithOutput(cmd)
}

type pipeClosingReadCloser struct {
	pipeclose func() error
	io.ReadCloser
}

func (p pipeClosingReadCloser) Close() error {
	if err := p.ReadCloser.Close(); err != nil {
		return err
	}
	if p.pipeclose != nil {
		if err := p.pipeclose(); err != nil {
			return errors.New(assertErrorType(err))
		}
	}
	return nil
}

func runCommandWithOutput(cmd *exec.Cmd) (io.ReadCloser, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return io.NopCloser(strings.NewReader(assertErrorType(err))), err
	}
	if err := cmd.Start(); err != nil {
		return io.NopCloser(strings.NewReader(assertErrorType(err))), err
	}

	return pipeClosingReadCloser{pipeclose: cmd.Wait, ReadCloser: stdout}, nil
}

func assertErrorType(err error) string {
	switch errType := err.(type) {
	case *exec.ExitError:
		return string(errType.Stderr)
	}
	return err.Error()
}
