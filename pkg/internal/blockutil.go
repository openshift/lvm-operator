package internal

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	ExecCommand          = exec.Command
	FilePathGlob         = filepath.Glob
	FilePathEvalSymLinks = filepath.EvalSymlinks
	mountFile            = "/proc/1/mountinfo"
)

// todo(rohan) copy over unit tests from LSO

const (
	// StateSuspended is a possible value of BlockDevice.State
	StateSuspended = "suspended"
	// DiskByIDDir is the path for symlinks to the device by id.
	DiskByIDDir = "/dev/disk/by-id/"
)

// IDPathNotFoundError indicates that a symlink to the device was not found in /dev/disk/by-id/
type IDPathNotFoundError struct {
	DeviceName string
}

func (e IDPathNotFoundError) Error() string {
	return fmt.Sprintf("IDPathNotFoundError: a symlink to  %q was not found in %q", e.DeviceName, DiskByIDDir)
}

// BlockDevice is the a block device as output by lsblk.
// All the fields are lsblk columns.
type BlockDevice struct {
	Name   string `json:"name"`
	KName  string `json:"kname"`
	Type   string `json:"type"`
	Model  string `json:"model,omitempty"`
	Vendor string `json:"vendor,omitempty"`
	State  string `json:"state,omitempty"`
	FSType string `json:"fsType"`
	Size   string `json:"size"`
	// Children   []BlockDevice `json:"children,omitempty"`
	Rotational string `json:"rota"`
	ReadOnly   string `json:"ro,omitempty"`
	Removable  string `json:"rm,omitempty"`
	PathByID   string `json:"pathByID,omitempty"`
	Serial     string `json:"serial,omitempty"`
	PartLabel  string `json:"partLabel,omitempty"`
}

// IDPathNotFoundError indicates that a symlink to the device was not found in /dev/disk/by-id/

// GetRotational as bool
func (b BlockDevice) GetRotational() (bool, error) {
	v, err := parseBitBool(b.Rotational)
	if err != nil {
		err = fmt.Errorf("failed to parse rotational property %q as bool: %w", b.Rotational, err)
	}
	return v, err
}

// GetReadOnly as bool
func (b BlockDevice) GetReadOnly() (bool, error) {
	v, err := parseBitBool(b.ReadOnly)
	if err != nil {
		err = fmt.Errorf("failed to parse readOnly property %q as bool: %w", b.ReadOnly, err)
	}
	return v, err
}

// GetRemovable as bool
func (b BlockDevice) GetRemovable() (bool, error) {
	v, err := parseBitBool(b.Removable)
	if err != nil {
		err = fmt.Errorf("failed to parse removable property %q as bool: %w", b.Removable, err)
	}
	return v, err
}

func parseBitBool(s string) (bool, error) {
	if s == "0" || s == "" {
		return false, nil
	} else if s == "1" {
		return true, nil
	}
	return false, fmt.Errorf("lsblk bool value not 0 or 1: %q", s)
}

// GetSize as int64
func (b BlockDevice) GetSize() (int64, error) {
	v, err := strconv.ParseInt(b.Size, 10, 64)
	if err != nil {
		err = fmt.Errorf("failed to parse size property %q as int64: %w", b.Size, err)
	}
	return v, err
}

// HasChildren check on BlockDevice
func (b BlockDevice) HasChildren() (bool, error) {
	sysDevDir := filepath.Join("/sys/block/", b.KName, "/*")
	paths, err := filepath.Glob(sysDevDir)
	if err != nil {
		return false, fmt.Errorf("failed to check if device %q has partitions: %w", b.KName, err)
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasPrefix(name, b.KName) {
			return true, nil
		}
	}
	return false, nil
}

// HasBindMounts checks for bind mounts and returns mount point for a device by parsing `proc/1/mountinfo`.
// HostPID should be set to true inside the POD spec to get details of host's mount points inside `proc/1/mountinfo`.
func (b BlockDevice) HasBindMounts() (bool, string, error) {
	data, err := ioutil.ReadFile(mountFile)
	if err != nil {
		return false, "", fmt.Errorf("failed to read file %s: %v", mountFile, err)
	}

	mountString := string(data)
	for _, mountInfo := range strings.Split(mountString, "\n") {
		if strings.Contains(mountInfo, b.KName) {
			mountInfoList := strings.Split(mountInfo, " ")
			if len(mountInfoList) >= 10 {
				// device source is 4th field for bind mounts and 10th for regular mounts
				if mountInfoList[3] == fmt.Sprintf("/%s", b.KName) || mountInfoList[9] == fmt.Sprintf("/dev/%s", b.KName) {
					return true, mountInfoList[4], nil
				}
			}
		}
	}

	return false, "", nil
}

// GetDevPath for block device (/dev/sdx)
func (b BlockDevice) GetDevPath() (string, error) {
	if b.KName == "" {
		return "", fmt.Errorf("empty KNAME")
	}
	return filepath.Join("/dev/", b.KName), nil
}

// GetPathByID check on BlockDevice
func (b BlockDevice) GetPathByID() (string, error) {

	// return if previously populated value is valid
	if len(b.PathByID) > 0 && strings.HasPrefix(b.PathByID, DiskByIDDir) {
		evalsCorrectly, err := PathEvalsToDiskLabel(b.PathByID, b.KName)
		if err == nil && evalsCorrectly {
			return b.PathByID, nil
		}
	}
	b.PathByID = ""
	diskByIDDir := filepath.Join(DiskByIDDir, "/*")
	paths, err := filepath.Glob(diskByIDDir)
	if err != nil {
		return "", fmt.Errorf("could not list files in %q: %w", DiskByIDDir, err)
	}
	for _, path := range paths {
		isMatch, err := PathEvalsToDiskLabel(path, b.KName)
		if err != nil {
			return "", err
		}
		if isMatch {
			b.PathByID = path
			return path, nil
		}
	}
	devPath, err := b.GetDevPath()
	if err != nil {
		return "", err
	}
	// return path by label and error
	return devPath, IDPathNotFoundError{DeviceName: b.KName}
}

// PathEvalsToDiskLabel checks if the path is a symplink to a file devName
func PathEvalsToDiskLabel(path, devName string) (bool, error) {
	devPath, err := FilePathEvalSymLinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("could not eval symLink %q:%w", devPath, err)
	}
	if filepath.Base(devPath) == devName {
		return true, nil
	}
	return false, nil
}

// ListBlockDevices using the lsblk command
func ListBlockDevices() ([]BlockDevice, []string, error) {
	// var output bytes.Buffer
	var blockDevices []BlockDevice

	deviceFSMap, err := GetDeviceFSMap()
	if err != nil {
		return []BlockDevice{}, []string{}, fmt.Errorf("failed to list block devices", err)
	}

	columns := "NAME,ROTA,TYPE,SIZE,MODEL,VENDOR,RO,RM,STATE,KNAME,SERIAL,PARTLABEL"
	args := []string{"--pairs", "-b", "-o", columns}
	cmd := ExecCommand("lsblk", args...)
	output, err := executeCmdWithCombinedOutput(cmd)
	if err != nil {
		return []BlockDevice{}, []string{}, err
	}
	badRows := make([]string, 0)
	// convert to json and then Marshal.
	outputMapList := make([]map[string]interface{}, 0)
	rowList := strings.Split(output, "\n")
	for _, row := range rowList {
		if len(strings.Trim(row, " ")) == 0 {
			break
		}
		outputMap := make(map[string]interface{})
		// split by `" ` to avoid splitting on spaces in MODEL,VENDOR
		keyValues := strings.Split(row, `" `)
		for _, keyValue := range keyValues {
			keyValueList := strings.Split(keyValue, "=")
			if len(keyValueList) != 2 {
				continue
			}
			key := strings.ToLower(keyValueList[0])
			value := strings.Replace(keyValueList[1], `"`, "", -1)
			outputMap[key] = strings.TrimSpace(value)
		}

		// only use device if name is populated, and non-empty
		v, found := outputMap["name"]
		if !found {
			badRows = append(badRows, row)
			break
		}
		name := v.(string)
		if len(strings.Trim(name, " ")) == 0 {
			badRows = append(badRows, row)
			break
		}

		// Update device filesystem using `blkid`
		if fs, ok := deviceFSMap[fmt.Sprintf("/dev/%s", name)]; ok {
			outputMap["fsType"] = fs
		}

		outputMapList = append(outputMapList, outputMap)
	}

	if len(badRows) == len(rowList) {
		return []BlockDevice{}, badRows, fmt.Errorf("could not parse any of the lsblk rows")
	}

	jsonBytes, err := json.Marshal(outputMapList)
	if err != nil {
		return []BlockDevice{}, badRows, err
	}

	err = json.Unmarshal(jsonBytes, &blockDevices)
	if err != nil {
		return []BlockDevice{}, badRows, err
	}

	return blockDevices, badRows, nil
}

// GetDeviceFSMap returns mapping between disks and the filesystem using blkid
// It parses the output of `blkid -s TYPE`. Sample output format before parsing
// `/dev/sdc: TYPE="ext4"
// /dev/sdd: TYPE="ext2"`
func GetDeviceFSMap() (map[string]string, error) {
	m := map[string]string{}
	args := []string{"-s", "TYPE"}
	cmd := ExecCommand("blkid", args...)
	output, err := executeCmdWithCombinedOutput(cmd)
	if err != nil {
		return map[string]string{}, err
	}
	lines := strings.Split(output, "\n")
	for _, l := range lines {
		if len(l) <= 0 {
			// Ignore empty line.
			continue
		}

		values := strings.Split(l, ":")
		if len(values) != 2 {
			continue
		}

		fs := strings.Split(values[1], "=")
		if len(fs) != 2 {
			continue
		}

		m[values[0]] = strings.Trim(strings.TrimSpace(fs[1]), "\"")
	}

	return m, nil
}

func executeCmdWithCombinedOutput(cmd *exec.Cmd) (string, error) {
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
