package logpassthrough

import (
	"fmt"

	"github.com/spf13/pflag"
)

type Bindable interface {
	// BindFlags binds all values against a given flag-set.
	BindFlags(fs *pflag.FlagSet)
}

type ArgPassable interface {
	// AsArgs outputs all fields that can be passed to a commandline.
	AsArgs() []string
}

// Options represents all pass-through options for logging verbosity
type Options struct {
	VGManager *VGmanagerOptions
}

// NewOptions creates a new option set and binds it's values against a given flagset.
func NewOptions() *Options {
	opts := &Options{
		VGManager: &VGmanagerOptions{},
	}
	return opts
}

func (o *Options) BindFlags(fs *pflag.FlagSet) {
	o.VGManager.BindFlags(fs)
}

// ZapOptions contains a list of all passed options from zap-logging
type ZapOptions struct {
	// ZapLogLevel is the Zap Level to configure the verbosity of logging. Can be one of 'debug', 'info', 'error', or any integer value > 0 which corresponds to custom debug levels of increasing verbosity
	ZapLogLevel string
}

func (o *ZapOptions) AsArgs() []string {
	var args []string
	if len(o.ZapLogLevel) > 0 {
		args = append(args, fmt.Sprintf("--%s=%s", "zap-log-level", o.ZapLogLevel))
	}
	return args
}

func (o *ZapOptions) BindFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ZapLogLevel, "zap-log-level", "",
		"Zap Level to configure the verbosity of logging. Can be one of 'debug', 'info', 'error', "+
			"or any integer value > 0 which corresponds to custom debug levels of increasing verbosity")
}

// KlogOptions contains a list of all passed options from the klog library
type KlogOptions struct {
	// V is number for the log level verbosity
	V string
	// VModule is comma-separated list of pattern=N settings for file-filtered logging
	VModule string
}

func (o *KlogOptions) AsArgs() []string {
	var args []string
	if len(o.V) > 0 {
		args = append(args, fmt.Sprintf("--%s=%s", "v", o.V))
	}
	if len(o.VModule) > 0 {
		args = append(args, fmt.Sprintf("--%s=%s", "vmodule", o.VModule))
	}
	return args
}

func (o *KlogOptions) BindFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.V, "v", "", "number for the log level verbosity")
	fs.StringVar(&o.VModule, "vmodule", "", "comma-separated list of pattern=N settings for file-filtered logging")
}

type VGmanagerOptions struct {
	KlogOptions
	ZapOptions
}

func (o *VGmanagerOptions) BindFlags(fs *pflag.FlagSet) {
	bindFlagsWithPrefix(&o.KlogOptions, fs, "vgmanager")
	bindFlagsWithPrefix(&o.ZapOptions, fs, "vgmanager")
}

func (o *VGmanagerOptions) AsArgs() []string {
	return append(o.ZapOptions.AsArgs(), o.KlogOptions.AsArgs()...)
}

// bindFlagsWithPrefix takes a given bindable and binds it to the flagset by
// 1. appending a prefix to the name of the argument to avoid collisions
// 2. appending a prefix to the usage description to identify it in the help documentation
func bindFlagsWithPrefix(bindable Bindable, fs *pflag.FlagSet, prefix string) {
	temp := pflag.NewFlagSet(prefix, pflag.ExitOnError)
	bindable.BindFlags(temp)
	temp.VisitAll(func(f *pflag.Flag) {
		f.Name = fmt.Sprintf("%s-%s", prefix, f.Name)
		f.Usage = fmt.Sprintf("%s: %s", prefix, f.Usage)
	})
	fs.AddFlagSet(temp)
}
