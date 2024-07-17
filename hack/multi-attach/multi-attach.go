package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"

	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	WorkerNodeRole       = "node-role.kubernetes.io/worker"
	DefaultAWSPurposeTag = "rh-lvms-testing"
	DefaultVolumeType    = "io2"
	DefaultVolumeSizeGB  = 100
	DefaultVolumeIOPS    = 3000
	DefaultVolumeName    = "lvms-multiattach-test"
)

var scheme = runtime.NewScheme()

// This small tool is written to automatically detect all worker nodes
// in an AWS cluster, then create a volume in EBS based on
// DefaultVolumeType, DefaultVolumeSizeGB, DefaultVolumeIOPS and DefaultVolumeName
// and attach it to all worker nodes via multi-attach, simulating a SAN on an AWS node.
func main() {
	utilruntime.Must(k8sscheme.AddToScheme(scheme))

	ctrl.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	ctx := ctrllog.IntoContext(context.Background(), ctrl.Log)
	logger := ctrllog.FromContext(ctx)

	err := run(ctx)

	if err != nil {
		logger.Error(err, "Error running multi-attach")
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	// Load Kubernetes configuration
	config, err := getKubeconfig(os.Getenv("KUBECONFIG"))
	if err != nil {
		return fmt.Errorf("could not get kubeconfig: %w", err)
	}

	// Create Kubernetes clnt
	clnt, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("could not create Kubernetes client: %w", err)
	}

	// List worker nodes
	nodes := &corev1.NodeList{}
	if err = clnt.List(ctx, nodes, client.MatchingLabels{WorkerNodeRole: ""}); err != nil {
		return fmt.Errorf("could not list worker nodes: %w", err)
	} else if len(nodes.Items) == 0 {
		return fmt.Errorf("no worker nodes found")
	}

	awsNodeInfo, err := getAWSNodeInfo(nodes.Items[0])
	if err != nil {
		return fmt.Errorf("could not get AWS node info: %w", err)
	}

	volume, err := getMultiAttachDiskSetupFromNodeList(nodes.Items)
	if err != nil {
		return fmt.Errorf("could not get node environment: %w", err)
	}

	ctrllog.FromContext(ctx).Info("preparing volume", "Node", volume)

	// Load AWS configuration
	ec2Client, err := getEC2Client(ctx, clnt, awsNodeInfo.Region)
	if err != nil {
		return fmt.Errorf("could not get EC2 client: %w", err)
	}

	diskMan := NewAWSDiskManager(ec2Client)

	if err = diskMan.cleanupAWSDisks(ctx, nodes.Items); err != nil {
		return fmt.Errorf("could not cleanup AWS disks: %w", err)
	}

	if err = diskMan.CreateAndAttachAWSVolumes(ctx, []Volume{volume}); err != nil {
		return fmt.Errorf("could not create and attach AWS volumes: %w", err)
	}

	return nil
}

func getEC2Client(ctx context.Context, client client.Client, region string) (*ec2.EC2, error) {
	logger := ctrllog.FromContext(ctx)
	// get AWS credentials
	awsCreds := &corev1.Secret{}
	secretName := types.NamespacedName{Name: "aws-creds", Namespace: "kube-system"}
	err := client.Get(ctx, secretName, awsCreds)
	if err != nil {
		return nil, fmt.Errorf("could not get aws credentials for EC2 Client: %w", err)
	}
	// detect region
	// base64 decode
	id, found := awsCreds.Data["aws_access_key_id"]
	if !found {
		return nil, fmt.Errorf("cloud credential id not found")
	}
	key, found := awsCreds.Data["aws_secret_access_key"]
	if !found {
		return nil, fmt.Errorf("cloud credential key not found")
	}
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(string(id), string(key), ""),
		Logger: aws.LoggerFunc(func(args ...interface{}) {
			logger.Info(fmt.Sprint(args), "source", "aws")
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("could not create session for ec2: %w", err)
	}

	// initialize client
	return ec2.New(sess), nil
}

func getKubeconfig(kubeconfig string) (*rest.Config, error) {
	var config *rest.Config
	var err error
	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else if config, err = clientcmd.BuildConfigFromKubeconfigGetter("", func() (*api.Config, error) {
		return clientcmd.NewDefaultClientConfigLoadingRules().Load()
	}); err != nil {
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, err
	}
	return config, err
}

type AWSNodeInfo struct {
	Name       string
	InstanceID string
	Region     string
	Zone       string
}

// getAWSNodeInfo returns instanceID, region, zone, error
func getAWSNodeInfo(node corev1.Node) (AWSNodeInfo, error) {
	// providerID looks like: aws:///us-east-2a/i-02d314dea14ed4efb
	if !strings.HasPrefix(node.Spec.ProviderID, "aws://") {
		return AWSNodeInfo{}, fmt.Errorf("%s is not an aws based Node: %s",
			node.GetName(), node.Spec.ProviderID)
	}
	split := strings.Split(node.Spec.ProviderID, "/")
	instanceID := split[len(split)-1]
	zone := split[len(split)-2]
	region := zone[:len(zone)-1]
	return AWSNodeInfo{
		Name:       node.GetName(),
		InstanceID: instanceID,
		Region:     region,
		Zone:       zone,
	}, nil
}

type Volume struct {
	Size  int
	Nodes []AWSNodeInfo
}

func getMultiAttachDiskSetupFromNodeList(nodes []corev1.Node) (Volume, error) {
	vol := Volume{
		Size: DefaultVolumeSizeGB,
	}

	for _, node := range nodes {
		nodeInfo, err := getAWSNodeInfo(node)
		if err != nil {
			return Volume{}, fmt.Errorf("could not get node environment: %w", err)
		}
		vol.Nodes = append(vol.Nodes, nodeInfo)
	}

	return vol, nil
}

type AWSDiskManager struct {
	ec2                    *ec2.EC2
	createTimeout          time.Duration
	createPollingInterval  time.Duration
	cleanupTimeout         time.Duration
	cleanupPollingInterval time.Duration
}

func NewAWSDiskManager(ec2 *ec2.EC2) *AWSDiskManager {
	return &AWSDiskManager{
		ec2:                    ec2,
		createTimeout:          4 * time.Minute,
		createPollingInterval:  5 * time.Second,
		cleanupTimeout:         5 * time.Minute,
		cleanupPollingInterval: 2 * time.Second,
	}
}

// CreateAndAttachAWSVolumes assumes that the device spaces /dev/sd[h-z] are available on the node
// do not provide more than 20 disksize
// do not use more than once per Node
// this function is async
func (m *AWSDiskManager) CreateAndAttachAWSVolumes(ctx context.Context, volumes []Volume) error {
	for _, disk := range volumes {
		err := m.createAndAttachAWSVolumesForNodes(ctx, disk)
		if err != nil {
			return fmt.Errorf("could not create and attach AWS volume for node: %w", err)
		}
	}
	return nil
}

func (m *AWSDiskManager) createAndAttachAWSVolumesForNodes(ctx context.Context, volume Volume) error {
	log := ctrllog.FromContext(ctx).WithValues("nodes", volume.Nodes)

	// create ec2 volumes
	createInput := &ec2.CreateVolumeInput{
		AvailabilityZone:   aws.String(volume.Nodes[0].Zone),
		Size:               aws.Int64(int64(volume.Size)),
		VolumeType:         aws.String(DefaultVolumeType),
		Iops:               aws.Int64(DefaultVolumeIOPS),
		MultiAttachEnabled: aws.Bool(true),
		TagSpecifications: []*ec2.TagSpecification{
			{
				ResourceType: aws.String("volume"),
				Tags: []*ec2.Tag{
					{
						Key:   aws.String("Name"),
						Value: aws.String(fmt.Sprintf(DefaultVolumeName)),
					},
					{
						Key:   aws.String("purpose"),
						Value: aws.String(DefaultAWSPurposeTag),
					},
				},
			},
		},
	}
	vol, err := m.ec2.CreateVolumeWithContext(ctx, createInput)
	if err != nil {
		return fmt.Errorf("failed to create aws Disks for Nodes %s with %v: %w", volume.Nodes, createInput, err)
	}
	log.Info("created volume", "size", volume.Size, "id", vol.VolumeId)
	// attach and poll for attachment to complete
	attachments := make([]*ec2.VolumeAttachment, 0, len(volume.Nodes))

	err = wait.PollUntilContextTimeout(ctx, m.createPollingInterval, m.createTimeout, true,
		func(ctx context.Context) (bool, error) {
			describeVolumeInput := &ec2.DescribeVolumesInput{
				VolumeIds: []*string{vol.VolumeId},
			}
			vols, err := m.ec2.DescribeVolumesWithContext(ctx, describeVolumeInput)
			if err != nil {
				return false, fmt.Errorf("failed to describe volumes to determine attachment completion: %w", err)
			}
			if len(vols.Volumes) == 0 {
				return false, fmt.Errorf("volume not found")
			}

			for _, node := range volume.Nodes {
				for _, attachment := range attachments {
					if *attachment.InstanceId == node.InstanceID {
						ctrllog.FromContext(ctx).Info("volume already attached to node", "node", node.InstanceID)
						continue
					}
				}

				log := log.WithValues("size", vol.Size, "id", vol.VolumeId)
				log.Info("volume attachment starting")
				attachInput := &ec2.AttachVolumeInput{
					VolumeId:   vol.VolumeId,
					InstanceId: aws.String(node.InstanceID),
					Device:     aws.String(fmt.Sprintf("/dev/sdg")),
				}
				attachment, err := m.ec2.AttachVolumeWithContext(ctx, attachInput)
				if err != nil {
					ctrllog.FromContext(ctx).Info("failed to attach volume, retrying...", "node", node.InstanceID)
					continue
				}
				attachments = append(attachments, attachment)
			}
			return len(attachments) == len(volume.Nodes), nil
		})
	if err != nil {
		return fmt.Errorf("failed to wait for volume attachment to complete for nodes %s: %w", volume.Nodes, err)
	}
	return nil
}

func (m *AWSDiskManager) cleanupAWSDisks(ctx context.Context, nodes []corev1.Node) error {
	logger := ctrllog.FromContext(ctx)

	var awsNodes []AWSNodeInfo
	for _, node := range nodes {
		nodeInfo, err := getAWSNodeInfo(node)
		if err != nil {
			return fmt.Errorf("could not get node environment: %w", err)
		}
		awsNodes = append(awsNodes, nodeInfo)
	}
	removed := make([]int, 0, len(awsNodes))

	err := wait.PollUntilContextTimeout(ctx, m.cleanupPollingInterval, m.cleanupTimeout, true, func(ctx context.Context) (bool, error) {
		volumes, err := m.getAWSTestVolumes(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to list AWS volumes for cleanup (deletion): %+v", err)
		}
		for _, volume := range volumes {
			if volume.State == nil {
				logger.Info("volume did not have a state", "id", volume.VolumeId)
				return false, nil
			}

			for i, node := range awsNodes {
				removedBefore := false
				for _, removed := range removed {
					if i == removed {
						removedBefore = true
						break
					}
				}
				if removedBefore {
					continue
				}
				if attachment, err := m.ec2.DetachVolumeWithContext(ctx, &ec2.DetachVolumeInput{
					VolumeId:   volume.VolumeId,
					InstanceId: aws.String(node.InstanceID),
				}); err != nil {
					logger.Error(err, "could not detach volume", "volume_attachment", attachment)
				}
				removed = append(removed, i)
			}

			if len(removed) != len(awsNodes) {
				logger.Info("not all instances have been detached yet", "remaining", awsNodes)
				return false, nil
			}

			if *volume.State != ec2.VolumeStateAvailable {
				logger.Info("waiting for volume to become available after detach", "desiredState", ec2.VolumeStateAvailable, "currentState", volume.State, "id", volume.VolumeId)
				return false, nil
			}

			logger.Info("deleting AWS Volume", "size", volume.Size, "id", volume.VolumeId)
			if deleteOut, err := m.ec2.DeleteVolumeWithContext(ctx, &ec2.DeleteVolumeInput{VolumeId: volume.VolumeId}); err != nil {
				logger.Error(err, "could not delete volume", "delete_out", deleteOut)
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed AWS Volume cleanup: %w", err)
	}
	return nil
}

func (m *AWSDiskManager) getAWSTestVolumes(ctx context.Context) ([]*ec2.Volume, error) {
	output, err := m.ec2.DescribeVolumesWithContext(ctx, &ec2.DescribeVolumesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:purpose"),
				Values: []*string{aws.String(DefaultAWSPurposeTag)},
			},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list AWS volumes: %w", err)
	}

	return output.Volumes, err

}
