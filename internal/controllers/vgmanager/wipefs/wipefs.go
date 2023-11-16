package wipefs

import (
	"fmt"

	"github.com/openshift/lvm-operator/internal/controllers/vgmanager/exec"
)

var (
	DefaultWipefs = "/usr/sbin/wipefs"
)

type Wipefs interface {
	Wipe(deviceName string) error
}

type HostWipefs struct {
	exec.Executor
	wipefs string
}

func NewDefaultHostWipefs() *HostWipefs {
	return NewHostWipefs(&exec.CommandExecutor{}, DefaultWipefs)
}

func NewHostWipefs(executor exec.Executor, wipefs string) *HostWipefs {
	return &HostWipefs{
		Executor: executor,
		wipefs:   wipefs,
	}
}

// Wipe wipes the device only if force delete flag is set
func (wipefs *HostWipefs) Wipe(deviceName string) error {
	if len(deviceName) == 0 {
		return fmt.Errorf("failed to wipe the device. Device name is empty")
	}

	args := []string{"--all", "--force"}
	args = append(args, deviceName)
	out, err := wipefs.ExecuteCommandWithOutputAsHost(wipefs.wipefs, args...)
	_ = out.Close()
	if err != nil {
		return fmt.Errorf("failed to wipe the device %q. %v", deviceName, err)
	}

	return nil
}
