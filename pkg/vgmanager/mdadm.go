package vgmanager

import (
	"fmt"
	"github.com/openshift/lvm-operator/api/v1alpha1"
	"github.com/openshift/lvm-operator/pkg/internal"
)

const mdadm = "/usr/sbin/mdadm"
const mdDevicePrefix = "/dev/md"

type MDADMRunner struct {
	exec   internal.Executor
	config *v1alpha1.RAIDConfig
}

func NewMDADMRunner(exec internal.Executor, config *v1alpha1.RAIDConfig) *MDADMRunner {
	return &MDADMRunner{
		exec:   exec,
		config: config,
	}
}

// mdadm --create --verbose /dev/md0 --raid-devices=2 /dev/vdb /dev/vdc --level=1 -R
func (r MDADMRunner) CreateRAID(devices []internal.BlockDevice) error {

	if len(devices) == 0 {
		return fmt.Errorf("failed to create RAID. Physical volume list is empty")
	}

	var devicesForRAID []string
	for _, device := range devices {
		devicesForRAID = append(devicesForRAID, device.KName)
	}

	level := 0
	switch r.config.Type {
	case v1alpha1.RAIDType1:
		fallthrough
	default:
		level = 1
	}

	args := []string{
		"--create",
		"-q",
		r.config.Name,
		fmt.Sprintf("--raid-devices=%v", r.config.Mirrors+1),
		fmt.Sprintf("--level=%v", level),
		"-R",
	}
	args = append(args, devicesForRAID...)

	res, err := r.exec.ExecuteCommandWithOutputAsHost(mdadm, args...)
	if err != nil {
		return fmt.Errorf("failed to create MDM Raid Device %s. %v: %s", r.config.Name, err, res)
	}

	return nil
}

// mdadm --stop /dev/md0
// mdadm --zero-superblock /dev/vdb /dev/vdc
// lvmdevices --deldev /dev/md0

func (r MDADMRunner) DeleteRAID() error {
	args := []string{
		"--stop",
		"-q",
		r.config.Name,
		"-R",
	}
	res, err := r.exec.ExecuteCommandWithOutputAsHost(mdadm, args...)
	if err != nil {
		return fmt.Errorf("failed to stop MDM Raid Device %s. %v: %s", r.config.Name, err, res)
	}

	return nil
}
