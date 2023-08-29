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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-logr/logr"
	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

type AWSDiskManager struct {
	ec2                    *ec2.EC2
	log                    logr.Logger
	createTimeout          time.Duration
	createPollingInterval  time.Duration
	cleanupTimeout         time.Duration
	cleanupPollingInterval time.Duration
}

type NodeDisks struct {
	Node  string
	Disks []Disk
	AWSNodeInfo
}

type Disk struct {
	Size int
}

type AWSNodeInfo struct {
	InstanceID string
	Region     string
	Zone       string
}

const (
	awsPurposeTag       = "rh-lvmo"
	labelNodeRoleWorker = "node-role.kubernetes.io/worker"
)

func NewAWSDiskManager(ec2 *ec2.EC2, log logr.Logger) *AWSDiskManager {
	return &AWSDiskManager{
		ec2:                    ec2,
		log:                    log,
		createTimeout:          4 * time.Minute,
		createPollingInterval:  5 * time.Second,
		cleanupTimeout:         5 * time.Minute,
		cleanupPollingInterval: 2 * time.Second,
	}
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
		InstanceID: instanceID,
		Region:     region,
		Zone:       zone,
	}, nil
}

// CreateAndAttachAWSVolumes assumes that the device spaces /dev/sd[h-z] are available on the node
// do not provide more than 20 disksize
// do not use more than once per Node
// this function is async
func (m *AWSDiskManager) CreateAndAttachAWSVolumes(ctx context.Context, disks []NodeDisks) error {
	for _, nodeDiskEntry := range disks {
		err := m.createAndAttachAWSVolumesForNode(ctx, nodeDiskEntry)
		if err != nil {
			return fmt.Errorf("could not create and attach AWS volume for node: %w", err)
		}
	}
	return nil
}

func (m *AWSDiskManager) createAndAttachAWSVolumesForNode(ctx context.Context, nodeEntry NodeDisks) error {
	log := m.log.WithValues("node", nodeEntry.Node)
	volumes := make([]*ec2.Volume, len(nodeEntry.Disks))
	volumeLetters := []string{"g", "h"}
	volumeIDs := make([]*string, 0)

	// create ec2 volumes
	for i, disk := range nodeEntry.Disks {
		diskSize := disk.Size
		diskName := fmt.Sprintf("sd%s", volumeLetters[i])
		createInput := &ec2.CreateVolumeInput{
			AvailabilityZone: aws.String(nodeEntry.Zone),
			Size:             aws.Int64(int64(diskSize)),
			VolumeType:       aws.String("gp2"),
			TagSpecifications: []*ec2.TagSpecification{
				{
					ResourceType: aws.String("volume"),
					Tags: []*ec2.Tag{
						{
							Key:   aws.String("Name"),
							Value: aws.String(diskName),
						},
						{
							Key:   aws.String("purpose"),
							Value: aws.String(awsPurposeTag),
						},
						{
							Key:   aws.String("chosen-instanceID"),
							Value: aws.String(nodeEntry.InstanceID),
						},
					},
				},
			},
		}
		volume, err := m.ec2.CreateVolumeWithContext(ctx, createInput)
		if err != nil {
			return fmt.Errorf("failed to create and attach aws Disks for Node %s with %v: %w",
				nodeEntry.Node, createInput, err)
		}
		log.Info("creating volume", "size", volume.Size, "id", volume.VolumeId)
		volumes[i] = volume
		volumeIDs = append(volumeIDs, volume.VolumeId)
	}
	// attach and poll for attachment to complete
	err := wait.PollUntilContextTimeout(ctx, m.createPollingInterval, m.createTimeout, true,
		func(ctx context.Context) (bool, error) {
			describeVolumeInput := &ec2.DescribeVolumesInput{
				VolumeIds: volumeIDs,
			}
			describedVolumes, err := m.ec2.DescribeVolumesWithContext(ctx, describeVolumeInput)
			if err != nil {
				return false, fmt.Errorf("failed to describe volumes to determine attachment completion: %w", err)
			}
			allAttached := true
			for i, volume := range describedVolumes.Volumes {
				log := log.WithValues("size", volume.Size, "id", volume.VolumeId)
				if *volume.State == ec2.VolumeStateInUse {
					log.Info("volume attachment complete")
					continue
				}
				allAttached = false
				if *volume.State == ec2.VolumeStateAvailable {
					log.Info("volume attachment starting")
					attachInput := &ec2.AttachVolumeInput{
						VolumeId:   volume.VolumeId,
						InstanceId: aws.String(nodeEntry.InstanceID),
						Device:     aws.String(fmt.Sprintf("/dev/sd%s", volumeLetters[i])),
					}
					if _, err = m.ec2.AttachVolumeWithContext(ctx, attachInput); err != nil {
						return false, fmt.Errorf("could not attach volume %s: %w", attachInput, err)
					}
				}
			}
			return allAttached, nil

		})
	if err != nil {
		return fmt.Errorf("failed to wait for volume attachment to complete for node %s: %w",
			nodeEntry.Node, err)
	}
	return nil
}

func getEC2Client(ctx context.Context, region string) (*ec2.EC2, error) {
	// get AWS credentials
	awsCreds := &corev1.Secret{}
	secretName := types.NamespacedName{Name: "aws-creds", Namespace: "kube-system"}
	err := crClient.Get(ctx, secretName, awsCreds)
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
			ginkgo.GinkgoLogr.Info(fmt.Sprint(args), "source", "aws")
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("could not create session for ec2: %w", err)
	}

	// initialize client
	return ec2.New(sess), nil
}

func (m *AWSDiskManager) cleanupAWSDisks(ctx context.Context) error {
	err := wait.PollUntilContextTimeout(ctx, m.cleanupPollingInterval, m.cleanupTimeout, true, func(ctx context.Context) (bool, error) {
		volumes, err := m.getAWSTestVolumes(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to list AWS volumes for cleanup (deletion): %+v", err)
		}
		for _, volume := range volumes {
			if volume.State == nil {
				m.log.Info("volume did not have a state", "id", volume.VolumeId)
				return false, nil
			}

			if *volume.State == ec2.VolumeStateInUse {
				m.log.Info("detaching AWS Volume", "size", volume.Size, "id", volume.VolumeId)
				if attachment, err := m.ec2.DetachVolumeWithContext(ctx, &ec2.DetachVolumeInput{VolumeId: volume.VolumeId}); err != nil {
					m.log.Error(err, "could not detach volume", "volume_attachment", attachment)
				}
				return false, nil
			}

			if *volume.State != ec2.VolumeStateAvailable {
				m.log.Info("waiting for volume to become available after detach", "desiredState", ec2.VolumeStateAvailable, "currentState", volume.State, "id", volume.VolumeId)
				return false, nil
			}

			m.log.Info("deleting AWS Volume", "size", volume.Size, "id", volume.VolumeId)
			if deleteOut, err := m.ec2.DeleteVolumeWithContext(ctx, &ec2.DeleteVolumeInput{VolumeId: volume.VolumeId}); err != nil {
				m.log.Error(err, "could not delete volume", "delete_out", deleteOut)
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
				Values: []*string{aws.String(awsPurposeTag)},
			},
		},
	})

	return output.Volumes, err

}
