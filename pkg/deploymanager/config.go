package deploymanager

import (
	"os"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	operatorv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	lvmv1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	scheme = runtime.NewScheme()
)

const InstallNamespace = "openshift-storage"

func init() {
	utilruntime.Must(k8sscheme.AddToScheme(scheme))
	utilruntime.Must(lvmv1.AddToScheme(scheme))
	utilruntime.Must(operatorv1.AddToScheme(scheme))
	utilruntime.Must(operatorv1alpha1.AddToScheme(scheme))
	utilruntime.Must(snapapi.AddToScheme(scheme))
}

type DeployManager struct {
	crClient crclient.Client
}

func (t *DeployManager) GetCrClient() crclient.Client {
	return t.crClient
}

func getKubeconfig(kubeconfig string) (*rest.Config, error) {
	var config *rest.Config
	var err error
	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return config, nil
	}
	return config, err
}

// NewDeployManager creates a DeployManager struct with default configuration
func NewDeployManager() (*DeployManager, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := getKubeconfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	crClient, err := crclient.New(config, crclient.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

	return &DeployManager{
		crClient: crClient,
	}, nil

}
