package lsblk

import (
	"context"
	"os"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

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
