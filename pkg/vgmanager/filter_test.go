package vgmanager

import (
	"testing"

	"github.com/openshift/lvm-operator/pkg/internal"
	"github.com/stretchr/testify/assert"
)

type filterTestCase struct {
	label     string
	device    internal.BlockDevice
	expected  bool
	expectErr bool
}

func TestNotReadOnly(t *testing.T) {
	testcases := []filterTestCase{
		{label: "tc empty string", device: internal.BlockDevice{ReadOnly: ""}, expected: true, expectErr: false},
		{label: "tc false", device: internal.BlockDevice{ReadOnly: "false"}, expected: true, expectErr: false},
		{label: "tc true", device: internal.BlockDevice{ReadOnly: "true"}, expected: false, expectErr: false},
		{label: "tc 0", device: internal.BlockDevice{ReadOnly: "0"}, expected: true, expectErr: false},
		{label: "tc 1", device: internal.BlockDevice{ReadOnly: "1"}, expected: false, expectErr: false},
		{label: "tc invalid string", device: internal.BlockDevice{ReadOnly: "test"}, expected: true, expectErr: true},
	}
	for _, tc := range testcases {
		result, err := FilterMap[notReadOnly](tc.device, nil)
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
		{label: "tc suspended", device: internal.BlockDevice{State: "suspended"}, expected: false, expectErr: false},
		{label: "tc live", device: internal.BlockDevice{State: "live"}, expected: true, expectErr: false},
		{label: "tc running", device: internal.BlockDevice{State: "running"}, expected: true, expectErr: false},
	}
	for _, tc := range testcases {
		result, err := FilterMap[notSuspended](tc.device, nil)
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
		{label: "tc no fs", device: internal.BlockDevice{FSType: ""}, expected: true, expectErr: false},
		{label: "tc xfs", device: internal.BlockDevice{FSType: "xfs"}, expected: false, expectErr: false},
		{label: "tc swap", device: internal.BlockDevice{FSType: "swap"}, expected: false, expectErr: false},
	}
	for _, tc := range testcases {
		result, err := FilterMap[noFilesystemSignature](tc.device, nil)
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
		{label: "tc child", device: internal.BlockDevice{Name: "dev1", Children: []internal.BlockDevice{{Name: "child1"}}}, expected: false, expectErr: false},
		{label: "tc no child", device: internal.BlockDevice{Name: "dev2", Children: []internal.BlockDevice{}}, expected: true, expectErr: false},
	}
	for _, tc := range testcases {
		result, err := FilterMap[noChildren](tc.device, nil)
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
		{label: "tc ROM", device: internal.BlockDevice{Name: "dev1", Type: "rom"}, expected: false, expectErr: false},
		{label: "tc Disk", device: internal.BlockDevice{Name: "dev2", Type: "disk"}, expected: true, expectErr: false},
	}
	for _, tc := range testcases {
		result, err := FilterMap[usableDeviceType](tc.device, nil)
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
		{label: "tc 1", device: internal.BlockDevice{Name: "dev1", PartLabel: ""}, expected: true, expectErr: false},
		{label: "tc 2", device: internal.BlockDevice{Name: "dev2", PartLabel: "abc"}, expected: true, expectErr: false},
		{label: "tc 3", device: internal.BlockDevice{Name: "dev3", PartLabel: "bios"}, expected: false, expectErr: false},
		{label: "tc 4", device: internal.BlockDevice{Name: "dev4", PartLabel: "BIOS"}, expected: false, expectErr: false},
		{label: "tc 5", device: internal.BlockDevice{Name: "dev5", PartLabel: "boot"}, expected: false, expectErr: false},
		{label: "tc 6", device: internal.BlockDevice{Name: "dev6", PartLabel: "BOOT"}, expected: false, expectErr: false},
	}
	for _, tc := range testcases {
		result, err := FilterMap[noBiosBootInPartLabel](tc.device, nil)
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
		{label: "tc 1", device: internal.BlockDevice{Name: "dev1", PartLabel: ""}, expected: true},
		{label: "tc 2", device: internal.BlockDevice{Name: "dev2", PartLabel: "abc"}, expected: true},
		{label: "tc 3", device: internal.BlockDevice{Name: "dev3", PartLabel: "reserved"}, expected: false},
		{label: "tc 4", device: internal.BlockDevice{Name: "dev4", PartLabel: "RESERVED"}, expected: false},
		{label: "tc 5", device: internal.BlockDevice{Name: "dev5", PartLabel: "Reserved"}, expected: false},
	}
	for _, tc := range testcases {
		result, err := FilterMap[noReservedInPartLabel](tc.device, nil)
		assert.NoError(t, err)
		assert.Equal(t, tc.expected, result)
	}
}
