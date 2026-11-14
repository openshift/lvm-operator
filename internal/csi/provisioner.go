package csi

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/csi-lib-utils/connection"
	"github.com/kubernetes-csi/external-provisioner/v6/pkg/capacity"
	"github.com/kubernetes-csi/external-provisioner/v6/pkg/capacity/topology"
	provisionctrl "github.com/kubernetes-csi/external-provisioner/v6/pkg/controller"
	_ "github.com/kubernetes-csi/external-provisioner/v6/pkg/features"
	"github.com/kubernetes-csi/external-provisioner/v6/pkg/owner"
	snapclientset "github.com/kubernetes-csi/external-snapshotter/client/v8/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/component-base/featuregate"
	csitrans "k8s.io/csi-translation-lib"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v13/controller"
)

const (
	ResyncPeriodOfCsiNodeInformer = 1 * time.Hour
)

type ProvisionerOptions struct {
	DriverName          string
	CSIEndpoint         string
	CSIOperationTimeout time.Duration
	Metrics             *connection.ExtendedCSIMetricsManager
}

type Provisioner struct {
	config  *rest.Config
	client  *http.Client
	options ProvisionerOptions
}

func (p *Provisioner) NeedLeaderElection() bool {
	return true
}

var _ manager.Runnable = &Provisioner{}
var _ manager.LeaderElectionRunnable = &Provisioner{}

func NewProvisioner(mgr manager.Manager, options ProvisionerOptions) *Provisioner {
	return &Provisioner{
		config:  mgr.GetConfig(),
		client:  mgr.GetHTTPClient(),
		options: options,
	}
}

func (p *Provisioner) Start(ctx context.Context) error {
	logger := log.FromContext(ctx)

	onLostConnection := func(ctx context.Context) bool {
		log.FromContext(ctx).Info("lost connection to csi driver, attempting to reconnect due to in tree provisioning...")
		return true
	}

	grpcClient, err := connection.Connect(ctx, p.options.CSIEndpoint,
		p.options.Metrics,
		connection.OnConnectionLoss(onLostConnection),
		connection.WithTimeout(p.options.CSIOperationTimeout))
	if err != nil {
		return err
	}
	defer grpcClient.Close() //nolint:errcheck,staticcheck

	pluginCapabilities, controllerCapabilities, err := provisionctrl.GetDriverCapabilities(grpcClient, p.options.CSIOperationTimeout)
	if err != nil {
		return err
	}

	clientset, err := kubernetes.NewForConfigAndClient(p.config, p.client)
	if err != nil {
		return err
	}
	snapClient, err := snapclientset.NewForConfigAndClient(p.config, p.client)
	if err != nil {
		return err
	}

	// Generate a unique ID for this provisioner
	timeStamp := time.Now().UnixNano() / int64(time.Millisecond)
	identity := strconv.FormatInt(timeStamp, 10) + "-" + strconv.Itoa(rand.Intn(10000)) + "-" + p.options.DriverName

	translator := csitrans.New()
	factory := informers.NewSharedInformerFactory(clientset, ResyncPeriodOfCsiNodeInformer)

	scLister := factory.Storage().V1().StorageClasses().Lister()
	csiNodeLister := factory.Storage().V1().CSINodes().Lister()
	nodeLister := factory.Core().V1().Nodes().Lister()
	claimLister := factory.Core().V1().PersistentVolumeClaims().Lister()
	vaLister := factory.Storage().V1().VolumeAttachments().Lister()

	// Create a topology store for tracking PVC-to-node topology mappings.
	// Only needed when the driver supports topology-aware provisioning.
	// We check the plugin capability directly instead of using provisionctrl.SupportsTopology()
	// because the latter also checks the Topology feature gate, which is GA (always enabled)
	// and may conflict with the k8s 1.37 feature gate registry.
	var topologyStore *provisionctrl.InMemoryStore
	if pluginCapabilities[csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS] {
		topologyStore = provisionctrl.NewInMemoryStore()
	}

	// Register Topology and VolumeAttributesClass feature gates as Beta to prevent
	// k8s 1.37 from pruning them. The external-provisioner v6 library registers them
	// as GA, but k8s 1.37 rejects/prunes GA gates, causing Enabled() to panic.
	// This is a workaround until the upstream library removes the dead feature gate checks.
	_ = utilfeature.DefaultMutableFeatureGate.Add(map[featuregate.Feature]featuregate.FeatureSpec{
		"Topology":              {Default: true, PreRelease: featuregate.Beta},
		"VolumeAttributesClass": {Default: true, PreRelease: featuregate.Beta},
	})

	provisioner := provisionctrl.NewCSIProvisioner(
		clientset,
		p.options.CSIOperationTimeout,
		identity,
		"pvc",
		-1,
		grpcClient,
		snapClient,
		p.options.DriverName,
		pluginCapabilities,
		controllerCapabilities,
		"",
		false,
		true,
		translator,
		scLister,
		csiNodeLister,
		nodeLister,
		claimLister,
		vaLister,
		nil, // referenceGrantLister
		false,
		"",
		nil, // nodeDeployment
		false,
		false,
		topologyStore,
	)

	var capacityController *capacity.Controller
	namespace := os.Getenv("NAMESPACE")
	if namespace == "" {
		return fmt.Errorf("need NAMESPACE env variable for CSIStorageCapacity objects")
	}
	podName := os.Getenv("NAME")
	if podName == "" {
		return fmt.Errorf("need NAME env variable to determine CSIStorageCapacity owner")
	}
	controllerRef, err := owner.Lookup(p.config, namespace, podName,
		schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Pod",
		}, 2)
	if err != nil {
		return fmt.Errorf("look up owner(s) of pod failed: %w", err)
	}
	logger.Info(fmt.Sprintf("using %s/%s %s as owner of CSIStorageCapacity objects", controllerRef.APIVersion, controllerRef.Kind, controllerRef.Name))
	topologyRateLimiter := workqueue.NewTypedItemExponentialFailureRateLimiter[string](time.Second, 5*time.Minute)

	topologyQueue := workqueue.NewTypedRateLimitingQueueWithConfig(topologyRateLimiter, workqueue.TypedRateLimitingQueueConfig[string]{
		Name: "csitopology",
	})

	topologyInformer := topology.NewNodeTopology(
		p.options.DriverName,
		clientset,
		factory.Core().V1().Nodes(),
		factory.Storage().V1().CSINodes(),
		topologyQueue,
	)

	managedByID := "external-provisioner"

	factoryForNamespace := informers.NewSharedInformerFactoryWithOptions(clientset,
		ResyncPeriodOfCsiNodeInformer,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(lo *metav1.ListOptions) {
			lo.LabelSelector = labels.Set{
				capacity.DriverNameLabel: p.options.DriverName,
				capacity.ManagedByLabel:  managedByID,
			}.AsSelector().String()
		}),
	)
	// We use the V1 CSIStorageCapacity API if available.
	clientFactory := capacity.NewV1ClientFactory(clientset)
	cInformer := factoryForNamespace.Storage().V1().CSIStorageCapacities()

	capacityRateLimiter := workqueue.NewTypedItemExponentialFailureRateLimiter[capacity.QueueKey](time.Second, 5*time.Minute)
	capacityController = capacity.NewCentralCapacityController(
		csi.NewControllerClient(grpcClient),
		p.options.DriverName,
		clientFactory,
		// Metrics for the queue is available in the default registry.
		workqueue.NewTypedRateLimitingQueueWithConfig(capacityRateLimiter, workqueue.TypedRateLimitingQueueConfig[capacity.QueueKey]{
			Name: "csistoragecapacity",
		}),
		controllerRef,
		managedByID,
		namespace,
		topologyInformer,
		factory.Storage().V1().StorageClasses(),
		cInformer,
		time.Minute,
		false,
		p.options.CSIOperationTimeout,
	)
	// Wrap Provision and Delete to detect when it is time to refresh capacity.
	provisioner = capacity.NewProvisionWrapper(provisioner, capacityController)

	claimRateLimiter := workqueue.NewTypedItemExponentialFailureRateLimiter[string](time.Second, 5*time.Minute)
	claimQueue := workqueue.NewTypedRateLimitingQueueWithConfig(claimRateLimiter, workqueue.TypedRateLimitingQueueConfig[string]{
		Name: "claims",
	})
	claimInformer := factory.Core().V1().PersistentVolumeClaims().Informer()

	provisionRateLimiter := workqueue.NewTypedItemExponentialFailureRateLimiter[string](time.Second, 5*time.Minute)
	provisionController := controller.NewProvisionController(
		ctx,
		clientset,
		p.options.DriverName,
		provisioner,
		controller.LeaderElection(false), // Always disable leader election in provisioner lib. Leader election should be done here in the CSI provisioner level instead.
		controller.FailedProvisionThreshold(0),
		controller.FailedDeleteThreshold(0),
		controller.RateLimiter(provisionRateLimiter),
		controller.Threadiness(1),
		controller.CreateProvisionedPVLimiter(workqueue.DefaultTypedControllerRateLimiter[string]()),
		controller.ClaimsInformer(claimInformer),
		controller.AddFinalizer(true),
	)

	csiClaimController := provisionctrl.NewCloningProtectionController(
		clientset,
		claimLister,
		claimInformer,
		claimQueue,
		controllerCapabilities,
	)

	factory.Start(ctx.Done())
	// Starting is enough, the capacity controller will
	// wait for sync.
	factoryForNamespace.Start(ctx.Done())
	cacheSyncResult := factory.WaitForCacheSync(ctx.Done())
	for _, v := range cacheSyncResult {
		if !v {
			return fmt.Errorf("failed to sync Informers")
		}
	}

	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		topologyInformer.RunWorker(ctx)
		logger.Info("topology informer finished shutdown")
	}()
	go func() {
		defer wg.Done()
		capacityController.Run(ctx, 1, &wg)
		logger.Info("capacity controller finished shutdown")
	}()
	go func() {
		defer wg.Done()
		csiClaimController.Run(ctx, 1, &wg)
		logger.Info("csi claim controller finished shutdown")
	}()
	go func() {
		defer wg.Done()
		defer topologyQueue.ShutDown()
		provisionController.Run(ctx)
		logger.Info("provisioner controller finished shutdown")
	}()

	wg.Wait()
	logger.Info("provisioner finished shutdown")

	return nil
}
