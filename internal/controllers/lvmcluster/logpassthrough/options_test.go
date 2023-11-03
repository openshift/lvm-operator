package logpassthrough

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

func TestCSISideCarOptions_BindFlags(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		assertOnOptions func(a *assert.Assertions, o *Options)
	}{
		{
			"pass-all",
			[]string{
				"--csi-sidecar-v=1",
				"--csi-sidecar-vmodule=bla=1",
				"--topolvm-controller-v=2",
				"--topolvm-controller-vmodule=bla=2",
				"--topolvm-controller-zap-log-level=debug",
				"--topolvm-node-v=3",
				"--topolvm-node-vmodule=bla=3",
				"--topolvm-node-zap-log-level=debug",
				"--vgmanager-v=4",
				"--vgmanager-vmodule=bla=4",
				"--vgmanager-zap-log-level=debug",
			},
			func(a *assert.Assertions, o *Options) {
				a.Equal(o.CSISideCar.VModule, "bla=1")
				a.Equal(o.CSISideCar.V, "1")
				a.Contains(o.CSISideCar.AsArgs(), "--v=1")
				a.Contains(o.CSISideCar.AsArgs(), "--vmodule=bla=1")

				a.Equal(o.TopoLVMController.VModule, "bla=2")
				a.Equal(o.TopoLVMController.V, "2")
				a.Equal(o.TopoLVMController.ZapLogLevel, "debug")
				a.Contains(o.TopoLVMController.AsArgs(), "--v=2")
				a.Contains(o.TopoLVMController.AsArgs(), "--vmodule=bla=2")
				a.Contains(o.TopoLVMController.AsArgs(), "--zap-log-level=debug")

				a.Equal(o.TopoLVMNode.VModule, "bla=3")
				a.Equal(o.TopoLVMNode.V, "3")
				a.Equal(o.TopoLVMNode.ZapLogLevel, "debug")
				a.Contains(o.TopoLVMNode.AsArgs(), "--v=3")
				a.Contains(o.TopoLVMNode.AsArgs(), "--vmodule=bla=3")
				a.Contains(o.TopoLVMNode.AsArgs(), "--zap-log-level=debug")

				a.Equal(o.VGManager.VModule, "bla=4")
				a.Equal(o.VGManager.V, "4")
				a.Equal(o.VGManager.ZapLogLevel, "debug")
				a.Contains(o.VGManager.AsArgs(), "--v=4")
				a.Contains(o.VGManager.AsArgs(), "--vmodule=bla=4")
				a.Contains(o.VGManager.AsArgs(), "--zap-log-level=debug")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := assert.New(t)
			o := NewOptions()
			flagSet := pflag.NewFlagSet(tt.name, pflag.ContinueOnError)

			o.BindFlags(flagSet)
			a.NoError(flagSet.Parse(tt.args))
			tt.assertOnOptions(a, o)
		})
	}
}
