package filter

import (
	"testing"

	"github.com/openshift/lvm-operator/pkg/lsblk"
	"github.com/stretchr/testify/assert"
)

type filterTestCase struct {
	label     string
	device    lsblk.BlockDevice
	expected  bool
	expectErr bool
}

func TestNotReadOnly(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc false", device: lsblk.BlockDevice{ReadOnly: false}, expected: true, expectErr: false},
		{label: "tc true", device: lsblk.BlockDevice{ReadOnly: true}, expected: false, expectErr: false},
	}
	for _, tc := range testcases {
		result, err := DefaultFilters(nil)[notReadOnly](tc.device, nil)
		assert.Equal(t, tc.expected, result)
		if tc.expectErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestNotSuspended(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc suspended", device: lsblk.BlockDevice{State: "suspended"}, expected: false, expectErr: false},
		{label: "tc live", device: lsblk.BlockDevice{State: "live"}, expected: true, expectErr: false},
		{label: "tc running", device: lsblk.BlockDevice{State: "running"}, expected: true, expectErr: false},
	}
	for _, tc := range testcases {
		result, err := DefaultFilters(nil)[notSuspended](tc.device, nil)
		assert.Equal(t, tc.expected, result)
		if tc.expectErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestNoFilesystemSignature(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc no fs", device: lsblk.BlockDevice{FSType: ""}, expected: true, expectErr: false},
		{label: "tc xfs", device: lsblk.BlockDevice{FSType: "xfs"}, expected: false, expectErr: false},
		{label: "tc swap", device: lsblk.BlockDevice{FSType: "swap"}, expected: false, expectErr: false},
	}
	for _, tc := range testcases {
		result, err := DefaultFilters(nil)[noValidFilesystemSignature](tc.device, nil)
		assert.Equal(t, tc.expected, result)
		if tc.expectErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestNoChildren(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc child", device: lsblk.BlockDevice{Name: "dev1", Children: []lsblk.BlockDevice{{Name: "child1"}}}, expected: false, expectErr: false},
		{label: "tc no child", device: lsblk.BlockDevice{Name: "dev2", Children: []lsblk.BlockDevice{}}, expected: true, expectErr: false},
	}
	for _, tc := range testcases {
		result, err := DefaultFilters(nil)[noChildren](tc.device, nil)
		assert.Equal(t, tc.expected, result)
		if tc.expectErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestIsUsableDeviceType(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc ROM", device: lsblk.BlockDevice{Name: "dev1", Type: "rom"}, expected: false, expectErr: false},
		{label: "tc Disk", device: lsblk.BlockDevice{Name: "dev2", Type: "disk"}, expected: true, expectErr: false},
	}
	for _, tc := range testcases {
		result, err := DefaultFilters(nil)[usableDeviceType](tc.device, nil)
		assert.Equal(t, tc.expected, result)
		if tc.expectErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestNoBiosBootInPartLabel(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc 1", device: lsblk.BlockDevice{Name: "dev1", PartLabel: ""}, expected: true, expectErr: false},
		{label: "tc 2", device: lsblk.BlockDevice{Name: "dev2", PartLabel: "abc"}, expected: true, expectErr: false},
		{label: "tc 3", device: lsblk.BlockDevice{Name: "dev3", PartLabel: "bios"}, expected: false, expectErr: false},
		{label: "tc 4", device: lsblk.BlockDevice{Name: "dev4", PartLabel: "BIOS"}, expected: false, expectErr: false},
		{label: "tc 5", device: lsblk.BlockDevice{Name: "dev5", PartLabel: "boot"}, expected: false, expectErr: false},
		{label: "tc 6", device: lsblk.BlockDevice{Name: "dev6", PartLabel: "BOOT"}, expected: false, expectErr: false},
	}
	for _, tc := range testcases {
		result, err := DefaultFilters(nil)[noBiosBootInPartLabel](tc.device, nil)
		assert.Equal(t, tc.expected, result)
		if tc.expectErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestNoReservedInPartLabel(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc 1", device: lsblk.BlockDevice{Name: "dev1", PartLabel: ""}, expected: true},
		{label: "tc 2", device: lsblk.BlockDevice{Name: "dev2", PartLabel: "abc"}, expected: true},
		{label: "tc 3", device: lsblk.BlockDevice{Name: "dev3", PartLabel: "reserved"}, expected: false},
		{label: "tc 4", device: lsblk.BlockDevice{Name: "dev4", PartLabel: "RESERVED"}, expected: false},
		{label: "tc 5", device: lsblk.BlockDevice{Name: "dev5", PartLabel: "Reserved"}, expected: false},
	}
	for _, tc := range testcases {
		result, err := DefaultFilters(nil)[noReservedInPartLabel](tc.device, nil)
		assert.NoError(t, err)
		assert.Equal(t, tc.expected, result)
	}
}
