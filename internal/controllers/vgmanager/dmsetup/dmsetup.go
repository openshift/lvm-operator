package dmsetup

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/exec"
)

var (
	DefaultDMSetup       = "/usr/sbin/dmsetup"
	ErrReferenceNotFound = errors.New("device-mapper reference not found")
)

type Dmsetup interface {
	Remove(deviceName string) error
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
func (dmsetup *HostDmsetup) Remove(deviceName string) error {
	if len(deviceName) == 0 {
		return errors.New("failed to remove device-mapper reference. Device name is empty")
	}

	args := []string{"remove"}
	args = append(args, deviceName)
	output, err := dmsetup.ExecuteCommandWithOutputAsHost(dmsetup.dmsetup, args...)
	if err != nil {
		if strings.Contains(output, "not found") {
			return ErrReferenceNotFound
		}
		return fmt.Errorf("failed to remove the reference from device-mapper %q: %v", deviceName, err)
	}

	return nil
}
