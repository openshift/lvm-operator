package main

import (
	"flag"
	"os"

	"github.com/go-logr/logr"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	configv1 "github.com/openshift/api/config/v1"
	secv1 "github.com/openshift/api/security/v1"
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	"github.com/openshift/lvm-operator/v4/cmd/operator"
	"github.com/openshift/lvm-operator/v4/cmd/vgmanager"
	"github.com/spf13/cobra"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {
	logr := ctrl.Log.WithName("setup")
	if err := NewCmd(logr).Execute(); err != nil {
		logr.Error(err, "fatal error encountered")
		os.Exit(1)
	}
}

// NewCmd creates a new CLI command
func NewCmd(setupLog logr.Logger) *cobra.Command {
	scheme := runtime.NewScheme()

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(lvmv1alpha1.AddToScheme(scheme))
	utilruntime.Must(topolvmv1.AddToScheme(scheme))
	utilruntime.Must(snapapi.AddToScheme(scheme))
	utilruntime.Must(secv1.Install(scheme))
	utilruntime.Must(configv1.Install(scheme))

	zapOpts := zap.Options{}
	zapFlagSet := flag.NewFlagSet("zap", flag.ExitOnError)
	zapOpts.BindFlags(zapFlagSet)

	klogFlagSet := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlagSet)

	cmd := &cobra.Command{
		Use:           "lvms",
		Short:         "Commands for running LVMS",
		Long:          `Contains commands that control various components reconciling of the main cluster resources within LVMS`,
		SilenceErrors: false,
		SilenceUsage:  true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			zapLogger := zap.New(zap.UseFlagOptions(&zapOpts))
			ctrl.SetLogger(zapLogger)
			klog.SetLogger(zapLogger)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.PersistentFlags().AddGoFlagSet(klogFlagSet)
	cmd.PersistentFlags().AddGoFlagSet(zapFlagSet)

	cmd.AddCommand(
		operator.NewCmd(&operator.Options{
			Scheme:   scheme,
			SetupLog: setupLog,
		}),
		vgmanager.NewCmd(&vgmanager.Options{
			Scheme:   scheme,
			SetupLog: setupLog,
		}),
	)

	return cmd
}
