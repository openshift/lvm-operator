/*
Copyright © 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"flag"
	"fmt"
	"os"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	configv1 "github.com/openshift/api/config/v1"
	secv1 "github.com/openshift/api/security/v1"
	lvmv1 "github.com/openshift/lvm-operator/api/v1alpha1"
	operatorv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// testNamespace is the namespace we run all the tests in.
	testNamespace = "lvm-endtoendtest"
	// installNamespace is the namespace we installs the operator in.
	installNamespace = "openshift-storage"
	// snapshotClass is the name of the lvm snapshot class.
	snapshotClass = "lvms-vg1"
)

var (
	// lvmCatalogSourceImage is the LVM CatalogSource container image to use in the deployment
	lvmCatalogSourceImage string
	// lvmSubscriptionChannel is the name of the lvm subscription channel
	lvmSubscriptionChannel string
	// diskInstall indicates whether disks are needed to be installed.
	diskInstall          bool
	lvmOperatorInstall   bool
	lvmOperatorUninstall bool
	scheme               = runtime.NewScheme()
	crClient             crclient.Client
	deserializer         runtime.Decoder
	contentTester        *PodRunner
)

func init() {
	flag.StringVar(&lvmCatalogSourceImage, "lvm-catalog-image", "", "The LVM CatalogSource container image to use in the deployment")
	flag.StringVar(&lvmSubscriptionChannel, "lvm-subscription-channel", "", "The subscription channel to revise updates from")
	flag.BoolVar(&lvmOperatorInstall, "lvm-operator-install", true, "Install the LVM operator before starting tests")
	flag.BoolVar(&lvmOperatorUninstall, "lvm-operator-uninstall", true, "Uninstall the LVM cluster and operator after test completion")
	flag.BoolVar(&diskInstall, "disk-install", false, "Create and attach disks to the nodes. This currently only works with AWS")

	utilruntime.Must(k8sscheme.AddToScheme(scheme))
	utilruntime.Must(lvmv1.AddToScheme(scheme))
	utilruntime.Must(operatorv1.AddToScheme(scheme))
	utilruntime.Must(operatorv1alpha1.AddToScheme(scheme))
	utilruntime.Must(snapapi.AddToScheme(scheme))
	utilruntime.Must(secv1.Install(scheme))
	utilruntime.Must(configv1.Install(scheme))

	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := getKubeconfig(kubeconfig)
	if err != nil {
		panic(fmt.Sprintf("Failed to set kubeconfig: %v", err))
	}

	crClient, err = crclient.New(config, crclient.Options{Scheme: scheme})
	if err != nil {
		panic(fmt.Sprintf("Failed to set client: %v", err))
	}

	if contentTester, err = NewPodRunner(config, crClient.Scheme()); err != nil {
		panic(fmt.Sprintf("Failed to initialize pod runner: %v", err))
	}

	deserializer = serializer.NewCodecFactory(scheme).UniversalDeserializer()
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
		return nil, err
	}
	return config, err
}
