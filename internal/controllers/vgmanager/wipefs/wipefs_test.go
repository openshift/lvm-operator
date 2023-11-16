package wipefs

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	mockExec "github.com/openshift/lvm-operator/internal/controllers/vgmanager/exec/test"
	"github.com/stretchr/testify/assert"
)

func TestWipe(t *testing.T) {
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
		MockExecuteCommandWithOutputAsHost: func(command string, args ...string) (io.ReadCloser, error) {
			if args[0] != "--all" || args[1] != "--force" {
				return io.NopCloser(strings.NewReader("")), fmt.Errorf("invalid args %q", args[0:2])
			}
			if args[2] == "/dev/loop1" {
				return io.NopCloser(strings.NewReader("")), nil
			} else if args[2] == "/dev/loop2" {
				return io.NopCloser(strings.NewReader("no such file or directory")), errors.New("no such file or directory")
			}
			return io.NopCloser(strings.NewReader("")), fmt.Errorf("invalid args %q", args[2])
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewHostWipefs(executor, DefaultWipefs).Wipe(tt.deviceName)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
