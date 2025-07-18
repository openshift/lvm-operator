package dmsetup

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	vgmanagerexec "github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/exec"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	DefaultDMSetup       = "/usr/sbin/dmsetup"
	ErrReferenceNotFound = errors.New("device-mapper reference not found")
)

type Dmsetup interface {
	Remove(ctx context.Context, deviceName string) error
}

type HostDmsetup struct {
	vgmanagerexec.Executor
	dmsetup string
}

func NewDefaultHostDmsetup() *HostDmsetup {
	return NewHostDmsetup(&vgmanagerexec.CommandExecutor{}, DefaultDMSetup)
}

func NewHostDmsetup(executor vgmanagerexec.Executor, dmsetup string) *HostDmsetup {
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

	output, err := dmsetup.CombinedOutputCommandAsHost(ctx, dmsetup.dmsetup, "remove", "--force", deviceName)

	if err == nil {
		log.FromContext(ctx).Info(fmt.Sprintf("successfully removed the reference from device-mapper %q: %s", deviceName, string(output)))
		return nil
	}

	if bytes.Contains(output, []byte("not found")) {
		return ErrReferenceNotFound
	}
	return fmt.Errorf("failed to remove the reference from device-mapper %q: %w", deviceName, err)
}
