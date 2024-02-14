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
				"--vgmanager-v=4",
				"--vgmanager-vmodule=bla=4",
				"--vgmanager-zap-log-level=debug",
			},
			func(a *assert.Assertions, o *Options) {
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
