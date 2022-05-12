package deploymanager

import (
	"os"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	olmclient "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	lvmv1 "github.com/red-hat-storage/lvm-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// InstallNamespace is the namespace lvm is installed into
const InstallNamespace = "openshift-storage"

// DeployManager is a util tool used by the tests
type DeployManager struct {
	olmClient      *olmclient.Clientset
	k8sClient      *kubernetes.Clientset
	crClient       crclient.Client
	parameterCodec runtime.ParameterCodec
	lvmClient      *rest.RESTClient
}

// GetCrClient is the function used to retrieve the controller-runtime client.
func (t *DeployManager) GetCrClient() crclient.Client {
	return t.crClient
}

// GetK8sClient is the function used to retrieve the kubernetes client.
func (t *DeployManager) GetK8sClient() *kubernetes.Clientset {
	return t.k8sClient
}

// GetParameterCodec is the function used to retrieve the parameterCodec.
func (t *DeployManager) GetParameterCodec() runtime.ParameterCodec {
	return t.parameterCodec
}

// GetLvmClient is the function used to retrieve the lvm client.
func (t *DeployManager) GetLvmClient() *rest.RESTClient {
	return t.lvmClient
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

// NewDeployManager creates a DeployManager struct with default configuration.
func NewDeployManager() (*DeployManager, error) {
	codecs := serializer.NewCodecFactory(scheme.Scheme)
	parameterCodec := runtime.NewParameterCodec(scheme.Scheme)
	var config *rest.Config
	var lvmConfig *rest.Config
	var olmConfig *rest.Config
	var err error

	kubeconfig := os.Getenv("KUBECONFIG")
	config, err = getKubeconfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	config.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: codecs}
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	// lvm Operator rest client
	lvmConfig, err = getKubeconfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	lvmConfig.GroupVersion = &lvmv1.GroupVersion
	lvmConfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: codecs}
	lvmConfig.APIPath = "/apis"
	lvmConfig.ContentType = runtime.ContentTypeJSON
	if lvmConfig.UserAgent == "" {
		lvmConfig.UserAgent = rest.DefaultKubernetesUserAgent()
	}
	lvmClient, err := rest.RESTClientFor(lvmConfig)
	if err != nil {
		return nil, err
	}

	// controller-runtime client
	myScheme := runtime.NewScheme()
	utilruntime.Must(lvmv1.AddToScheme(myScheme))
	utilruntime.Must(scheme.AddToScheme(myScheme))
	utilruntime.Must(snapapi.AddToScheme(myScheme))
	crClient, err := crclient.New(config, crclient.Options{Scheme: myScheme})
	if err != nil {
		return nil, err
	}

	// olm client
	olmConfig, err = getKubeconfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	olmConfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}
	olmConfig.APIPath = "/apis"
	olmConfig.ContentType = runtime.ContentTypeJSON
	olmClient, err := olmclient.NewForConfig(olmConfig)
	if err != nil {
		return nil, err
	}

	return &DeployManager{
		olmClient:      olmClient,
		k8sClient:      k8sClient,
		crClient:       crClient,
		lvmClient:      lvmClient,
		parameterCodec: parameterCodec,
	}, nil
}
