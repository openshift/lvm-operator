/*
Copyright 2021.

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

package vgmanager

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/red-hat-storage/lvm-operator/pkg/internal"
)

const (
	// todo(rohan): make these paths configurable. I think they depend on the host OS
	// this is why I copied the methods and didn't just import them from lvmd
	lvmPath     = "/usr/sbin/lvm"
	nsenterPath = "/usr/bin/nsenter"
)

type LVMAttr string

var (
	VGName LVMAttr = "vg_name"
)

// LVInfo is a map of lv attributes to values.
type LVInfo map[LVMAttr]string

// ErrNotFound is returned when a VG or LV is not found.
var ErrNotFound = errors.New("not found")

// VolumeGroup represents a volume group of linux lvm.
type VolumeGroup struct {
	name string
}

// Name returns the volume group name.
func (g *VolumeGroup) Name() string {
	return g.name
}

func (r *VGReconciler) addMatchingDevicesToVG(matchingDevices []internal.BlockDevice, vgName string) error {
	if len(matchingDevices) < 1 {
		return fmt.Errorf("can't create vg with 0 devices")
	}
	vgLogger := r.Log.WithValues("vgname", vgName)
	// for vgextend and vgcreate, args takes the form of VGName,dev1,dev2,dev3...
	var cmd string
	args := []string{vgName}
	for _, device := range matchingDevices {
		args = append(args, fmt.Sprintf("/dev/%s", device.KName))
	}

	_, err := FindVolumeGroup(vgName)
	if errors.Is(err, ErrNotFound) {
		// create vg
		vgLogger.Info("vg not found, creating")
		cmd = "/usr/sbin/vgcreate"
	} else if err != nil {
		return fmt.Errorf("error while looking for volumegroup %q: %w", vgName, err)
	} else {
		vgLogger.Info("vg found, extending")
		cmd = "/usr/sbin/vgextend"
	}

	c := runCommandAsHost(cmd, args...)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err = c.Run()
	if err != nil {
		vgLogger.Error(err, "error while adding pvs to volumegroup", "stdout", stdout.String(), "stderr", stderr.String())
		return err
	}

	return nil
}
func runCommandAsHost(cmd string, args ...string) *exec.Cmd {
	args = append([]string{"-m", "-u", "-i", "-n", "-p", "-t", "1", cmd}, args...)
	cmd = nsenterPath
	c := exec.Command(cmd, args...)
	return c
}

// FindVolumeGroup finds a named volume group.
// name is volume group name to look up.
func FindVolumeGroup(name string) (*VolumeGroup, error) {
	groups, err := ListVolumeGroups()
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		if group.Name() == name {
			return group, nil
		}
	}
	return nil, ErrNotFound
}

// ListVolumeGroups lists all volume groups.
func ListVolumeGroups() ([]*VolumeGroup, error) {
	infoList, err := parseOutput("vgs", "vg_name")
	if err != nil {
		return nil, err
	}
	groups := []*VolumeGroup{}
	for _, info := range infoList {
		groups = append(groups, &VolumeGroup{info["vg_name"]})
	}
	return groups, nil
}

// wrapExecCommand calls cmd with args but wrapped to run
// on the host
func wrapExecCommand(cmd string, args ...string) *exec.Cmd {
	args = append([]string{"-m", "-u", "-i", "-n", "-p", "-t", "1", cmd}, args...)
	cmd = nsenterPath
	c := exec.Command(cmd, args...)
	return c
}

// CallLVM calls lvm sub-commands.
// cmd is a name of sub-command.
func CallLVM(cmd string, args ...string) error {
	args = append([]string{cmd}, args...)
	c := wrapExecCommand(lvmPath, args...)
	c.Stderr = os.Stderr
	return c.Run()
}

// parseLines parses output from lvm.
func parseLines(output string) []LVInfo {
	ret := []LVInfo{}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		info := parseOneLine(line)
		ret = append(ret, info)
	}
	return ret
}
func parseOneLine(line string) LVInfo {
	ret := LVInfo{}
	line = strings.TrimSpace(line)
	for _, token := range strings.Split(line, " ") {
		if len(token) == 0 {
			continue
		}
		// assume token is "k=v"
		kv := strings.Split(token, "=")
		k, v := kv[0], kv[1]
		// k[5:] removes "LVM2_" prefix.
		k = strings.ToLower(k[5:])
		// assume v is 'some-value'
		v = strings.ToLower(strings.Trim(v, "'"))
		ret[LVMAttr(k)] = v
	}
	return ret
}

// parseOutput calls lvm family and parses output from it.
//
// cmd is a command name of lvm family.
// fields are comma separated field names.
// args is optional arguments for lvm command.
func parseOutput(cmd, fields string, args ...string) ([]LVInfo, error) {
	arg := []string{
		cmd, "-o", fields,
		"--noheadings", "--separator= ",
		"--units=b", "--nosuffix",
		"--unbuffered", "--nameprefixes",
	}
	arg = append(arg, args...)
	c := wrapExecCommand(lvmPath, arg...)
	c.Stderr = os.Stderr
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	out, err := io.ReadAll(stdout)
	if err != nil {
		return nil, err
	}
	if err := c.Wait(); err != nil {
		return nil, err
	}
	return parseLines(string(out)), nil
}
