package wipefs

import (
	"context"
	"errors"
	"fmt"

	vgmanagerexec "github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/exec"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	DefaultWipefs = "/usr/sbin/wipefs"
)

type Wipefs interface {
	Wipe(ctx context.Context, deviceName string) error
}

type HostWipefs struct {
	vgmanagerexec.Executor
	wipefs string
}

func NewDefaultHostWipefs() *HostWipefs {
	return NewHostWipefs(&vgmanagerexec.CommandExecutor{}, DefaultWipefs)
}

func NewHostWipefs(executor vgmanagerexec.Executor, wipefs string) *HostWipefs {
	return &HostWipefs{
		Executor: executor,
		wipefs:   wipefs,
	}
}

// Wipe wipes the device only if force delete flag is set
func (wipefs *HostWipefs) Wipe(ctx context.Context, deviceName string) error {
	if len(deviceName) == 0 {
		return fmt.Errorf("failed to wipe the device. Device name is empty")
	}
	if output, err := wipefs.CombinedOutputCommandAsHost(ctx, wipefs.wipefs, "--all", "--force", deviceName); err != nil {
		return fmt.Errorf("failed to wipe the device %q. %v", deviceName, errors.Join(err, errors.New(string(output))))
	} else {
		log.FromContext(ctx).Info(fmt.Sprintf("successfully wiped the device %q: %s", deviceName, string(output)))
	}
	return nil
}
