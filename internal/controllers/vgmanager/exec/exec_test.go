package exec

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func TestExecutor(t *testing.T) {
	a := assert.New(t)
	if os.Getuid() != 0 {
		t.Skip("run as root")
	}
	executor := &CommandExecutor{}

	t.Run("simple lvm version should succeed", func(t *testing.T) {
		ctx := log.IntoContext(context.Background(), testr.New(t))
		dataStream, err := executor.StartCommandWithOutputAsHost(ctx, "lvm", "version")
		a.NoError(err, "version should succeed")

		data, err := io.ReadAll(dataStream)
		a.NoError(err, "data should be readable from io stream")
		a.NoError(dataStream.Close(), "data stream should close without problems")
		a.Contains(string(data), "LVM version")
	})

	t.Run("simple lvm vgcreate with non existing device should fail and show logs", func(t *testing.T) {
		ctx := log.IntoContext(context.Background(), testr.New(t))
		dataStream, err := executor.StartCommandWithOutputAsHost(ctx, "lvm", "vgcreate", "test-vg", "/dev/does-not-exist")
		a.NoError(err, "vgcreate should not fail instantly as read didn't finish")
		data, err := io.ReadAll(dataStream)
		a.NoError(err, "data should be readable from io stream")
		a.Len(data, 0, "data should be empty as the command should fail")
		err = dataStream.Close()
		a.Error(err, "data stream should fail to close")

		lvmErr, ok := AsExecError(err)
		a.True(ok, "error should be a LVM error")
		a.NotNil(lvmErr, "error should not be nil")
		a.Equal(lvmErr.ExitCode(), 5, "exit code should be 5")
		a.ErrorContains(lvmErr, "exit status 5")
		a.ErrorContains(lvmErr, "No device found for /dev/does-not-exist")
		a.NotNil(dataStream, "data stream should be nil")
	})
}
