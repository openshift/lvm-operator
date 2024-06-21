package wipefs

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-logr/logr/testr"
	mockExec "github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/exec/test"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
		MockRunCommandAsHost: func(ctx context.Context, command string, args ...string) error {
			if args[0] != "--all" || args[1] != "--force" {
				return fmt.Errorf("invalid args %q", args[0:2])
			}
			if args[2] == "/dev/loop1" {
				return nil
			} else if args[2] == "/dev/loop2" {
				return errors.New("no such file or directory")
			}
			return fmt.Errorf("invalid args %q", args[2])
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			err := NewHostWipefs(executor, DefaultWipefs).Wipe(ctx, tt.deviceName)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
