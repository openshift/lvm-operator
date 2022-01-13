package internal

import (
	"fmt"
	"os/exec"
	"strings"
)

var (
	nsenterPath = "/usr/bin/nsenter"
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
