package dmsetup

import (
	"errors"
	"fmt"
	"testing"

	mockExec "github.com/openshift/lvm-operator/internal/controllers/vgmanager/exec/test"
	"github.com/stretchr/testify/assert"
)

func TestRemove(t *testing.T) {
	tests := []struct {
		name       string
		deviceName string
		wantErr    bool
	}{
		{"Empty device name", "", true},
		{"Existing device name", "/dev/loop1", false},
		{"Non-existing device name", "/dev/loop2", true},
	}

	executor := &mockExec.MockExecutor{
		MockExecuteCommandWithOutputAsHost: func(command string, args ...string) (string, error) {
			if args[0] != "remove" {
				return "", fmt.Errorf("invalid args %q", args[0])
			}
			if args[1] == "/dev/loop1" {
				return "", nil
			} else if args[1] == "/dev/loop2" {
				return "device loop2 not found", errors.New("device loop2 not found")
			}
			return "", fmt.Errorf("invalid args %q", args[1])
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewHostDmsetup(executor, DefaultDMSetup).Remove(tt.deviceName)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
