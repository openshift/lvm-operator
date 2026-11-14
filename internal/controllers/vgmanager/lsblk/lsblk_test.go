package lsblk

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func TestBlockDeviceMountpointsJSON(t *testing.T) {
	var device BlockDevice
	assert.NoError(t, json.Unmarshal([]byte(`{"kname":"/dev/sda","maj:min":"8:0","mountpoints":[null]}`), &device))
	if assert.NotNil(t, device.Mountpoints) {
		assert.Len(t, *device.Mountpoints, 1)
		assert.Nil(t, (*device.Mountpoints)[0])
	}
	assert.Equal(t, "8:0", device.MajorMinor)
	assert.NoError(t, json.Unmarshal([]byte(`{"kname":"/dev/sdb","mountpoints":[]}`), &device))
	if assert.NotNil(t, device.Mountpoints) {
		assert.Empty(t, *device.Mountpoints)
	}
}

func TestHostLSBLK(t *testing.T) {
	if os.Getuid() != 0 || os.Getenv("NON_ROOT") == "true" {
		t.Skip("run as root")
	}
	ctx := log.IntoContext(context.Background(), testr.New(t))
	a := assert.New(t)

	lsblk := NewDefaultHostLSBLK()

	devices, err := lsblk.ListBlockDevices(ctx)
	a.NoError(err)
	a.NotEmpty(devices)
}
