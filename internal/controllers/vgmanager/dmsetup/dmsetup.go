package dmsetup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/exec"
)

var (
	DefaultDMSetup       = "/usr/sbin/dmsetup"
	ErrReferenceNotFound = errors.New("device-mapper reference not found")
)

type Dmsetup interface {
	Remove(ctx context.Context, deviceName string) error
}

type HostDmsetup struct {
	exec.Executor
	dmsetup string
}

func NewDefaultHostDmsetup() *HostDmsetup {
	return NewHostDmsetup(&exec.CommandExecutor{}, DefaultDMSetup)
}

func NewHostDmsetup(executor exec.Executor, dmsetup string) *HostDmsetup {
	return &HostDmsetup{
		Executor: executor,
		dmsetup:  dmsetup,
	}
}

// Remove removes the device's reference from the device-mapper
func (dmsetup *HostDmsetup) Remove(ctx context.Context, deviceName string) error {
	if len(deviceName) == 0 {
		return errors.New("failed to remove device-mapper reference. Device name is empty")
	}

	args := []string{"remove"}
	args = append(args, deviceName)
	output, err := dmsetup.StartCommandWithOutputAsHost(ctx, dmsetup.dmsetup, args...)
	if err == nil {
		return nil
	}

	// if err != nil (ExitCode != 0), we can check the cmd output to verify if we have a non-found device
	data, err := io.ReadAll(output)
	if err != nil {
		return fmt.Errorf("failed to read output from device-mapper %q: %w", deviceName, err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("failed to close output from device-mapper %q: %w", deviceName, err)
	}

	if bytes.Contains(data, []byte("not found")) {
		return ErrReferenceNotFound
	}
	return fmt.Errorf("failed to remove the reference from device-mapper %q: %w", deviceName, err)
}
