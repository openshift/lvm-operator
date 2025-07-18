package dmsetup

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
		MockCombinedOutputCommandAsHost: func(ctx context.Context, command string, args ...string) ([]byte, error) {
			if args[0] != "remove" {
				return nil, fmt.Errorf("invalid args %q", args[0])
			}
			if args[1] != "--force" {
				return nil, fmt.Errorf("invalid args %q", args[1])
			}
			switch args[2] {
			case "/dev/loop1":
				return nil, nil
			case "/dev/loop2":
				return []byte("device loop2 not found"), errors.New("device loop2 not found")
			}
			return nil, fmt.Errorf("invalid args %q", args[1])
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			err := NewHostDmsetup(executor, DefaultDMSetup).Remove(ctx, tt.deviceName)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
