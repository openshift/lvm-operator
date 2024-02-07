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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	nsenterPath = "/usr/bin/nsenter"
)

// Executor is the interface for running exec commands
type Executor interface {
	StartCommandWithOutputAsHost(ctx context.Context, command string, arg ...string) (io.ReadCloser, error)
	RunCommandAsHost(ctx context.Context, command string, arg ...string) error
	RunCommandAsHostInto(ctx context.Context, into any, command string, arg ...string) error
}

// CommandExecutor is an Executor type
type CommandExecutor struct{}

// RunCommandAsHost executes a command as host and returns an error if the command fails.
// it finishes the run and the output will be printed to the log.
func (e *CommandExecutor) RunCommandAsHost(ctx context.Context, command string, arg ...string) error {
	return e.RunCommandAsHostInto(ctx, nil, command, arg...)
}

// RunCommandAsHostInto executes a command as host and returns an error if the command fails.
// it finishes the run and decodes the output via JSON into the provided struct pointer.
// if the struct pointer is nil, the output will be printed to the log instead.
func (e *CommandExecutor) RunCommandAsHostInto(ctx context.Context, into any, command string, arg ...string) error {
	output, err := e.StartCommandWithOutputAsHost(ctx, command, arg...)
	if err != nil {
		return fmt.Errorf("failed to execute command: %v", err)
	}

	// if we don't decode the output into a struct, we can still log the command results from stdout.
	if into == nil {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			log.FromContext(ctx).V(1).Info(strings.TrimSpace(scanner.Text()))
		}
		err = scanner.Err()
	} else {
		err = json.NewDecoder(output).Decode(&into)
	}
	closeErr := output.Close()

	return errors.Join(closeErr, err)
}

// StartCommandWithOutputAsHost executes a command with output as host and returns the output as a ReadCloser.
// The caller is responsible for closing the ReadCloser.
// Not calling close on this method will result in a resource leak.
func (*CommandExecutor) StartCommandWithOutputAsHost(ctx context.Context, command string, arg ...string) (io.ReadCloser, error) {
	args := append([]string{"-m", "-u", "-i", "-n", "-p", "-t", "1", command}, arg...)
	cmd := exec.Command(nsenterPath, args...)
	log.FromContext(ctx).V(1).Info("executing", "command", cmd.String())
	return runCommandWithOutput(cmd)
}

type pipeClosingReadCloser struct {
	pipeclose func() error
	io.ReadCloser
	stderr io.ReadCloser
}

func (p pipeClosingReadCloser) Close() error {
	if err := p.ReadCloser.Close(); err != nil {
		return err
	}

	// Read the stderr output after the read has finished since we are sure by then the command must have run.
	stderr, err := io.ReadAll(p.stderr)
	if err != nil {
		return err
	}

	if err := p.pipeclose(); err != nil {
		// wait can result in an exit code error
		return &internalError{
			err:    err,
			stderr: bytes.TrimSpace(stderr),
		}
	}
	return nil
}

func runCommandWithOutput(cmd *exec.Cmd) (io.ReadCloser, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return pipeClosingReadCloser{pipeclose: cmd.Wait, ReadCloser: stdout, stderr: stderr}, nil
}

// AsExecError returns the Error from the error if it exists and a bool indicating if is an Error or not.
func AsExecError(err error) (Error, bool) {
	var lvmErr Error
	ok := errors.As(err, &lvmErr)
	return lvmErr, ok
}

// Error is an error that wraps the original error and the stderr output of the command if found.
// It also provides an exit code if present that can be used to determine the type of error.
type Error interface {
	error
	ExitCode() int
	Unwrap() error
}

type internalError struct {
	err    error
	stderr []byte
}

func (e *internalError) Error() string {
	if e.stderr != nil {
		return fmt.Sprintf("%v: %v", e.err, string(bytes.TrimSpace(e.stderr)))
	}
	return e.err.Error()
}

func (e *internalError) Unwrap() error {
	return e.err
}

func (e *internalError) ExitCode() int {
	type exitError interface {
		ExitCode() int
		error
	}
	var err exitError
	if errors.As(e.err, &err) {
		return err.ExitCode()
	}
	return -1
}
