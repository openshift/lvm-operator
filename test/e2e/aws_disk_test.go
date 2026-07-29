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
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsCfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

type AWSDiskManager struct {
	ec2                    *ec2.Client
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

func NewAWSDiskManager(ec2Client *ec2.Client, log logr.Logger) *AWSDiskManager {
	return &AWSDiskManager{
		ec2:                    ec2Client,
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
	volumeLetters := []string{"g", "h"}
	volumeIDs := make([]string, 0)

	for i, disk := range nodeEntry.Disks {
		diskName := fmt.Sprintf("sd%s", volumeLetters[i])
		createInput := &ec2.CreateVolumeInput{
			AvailabilityZone: aws.String(nodeEntry.Zone),
			Size:             aws.Int32(int32(disk.Size)),
			VolumeType:       ec2types.VolumeTypeGp2,
			TagSpecifications: []ec2types.TagSpecification{
				{
					ResourceType: ec2types.ResourceTypeVolume,
					Tags: []ec2types.Tag{
						{Key: aws.String("Name"), Value: aws.String(diskName)},
						{Key: aws.String("purpose"), Value: aws.String(awsPurposeTag)},
						{Key: aws.String("chosen-instanceID"), Value: aws.String(nodeEntry.InstanceID)},
					},
				},
			},
		}
		volume, err := m.ec2.CreateVolume(ctx, createInput)
		if err != nil {
			return fmt.Errorf("failed to create and attach aws Disks for Node %s with %v: %w",
				nodeEntry.Node, createInput, err)
		}
		log.Info("creating volume", "size", volume.Size, "id", volume.VolumeId)
		volumeIDs = append(volumeIDs, *volume.VolumeId)
	}

	err := wait.PollUntilContextTimeout(ctx, m.createPollingInterval, m.createTimeout, true,
		func(ctx context.Context) (bool, error) {
			describedVolumes, err := m.ec2.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{
				VolumeIds: volumeIDs,
			})
			if err != nil {
				return false, fmt.Errorf("failed to describe volumes to determine attachment completion: %w", err)
			}
			allAttached := true
			for i, volume := range describedVolumes.Volumes {
				log := log.WithValues("size", volume.Size, "id", volume.VolumeId)
				if volume.State == ec2types.VolumeStateInUse {
					log.Info("volume attachment complete")
					continue
				}
				allAttached = false
				if volume.State == ec2types.VolumeStateAvailable {
					log.Info("volume attachment starting")
					attachInput := &ec2.AttachVolumeInput{
						VolumeId:   volume.VolumeId,
						InstanceId: aws.String(nodeEntry.InstanceID),
						Device:     aws.String(fmt.Sprintf("/dev/sd%s", volumeLetters[i])),
					}
					if _, err = m.ec2.AttachVolume(ctx, attachInput); err != nil {
						return false, fmt.Errorf("could not attach volume %s: %w", *volume.VolumeId, err)
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

func getEC2Client(region string) (*ec2.Client, error) {
	cfg, err := awsCfg.LoadDefaultConfig(context.Background(),
		awsCfg.WithRegion(region),
		awsCfg.WithSharedCredentialsFiles([]string{
			os.Getenv("CLUSTER_PROFILE_DIR") + "/.awscred",
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("could not load AWS config for ec2: %w", err)
	}
	return ec2.NewFromConfig(cfg), nil
}

func (m *AWSDiskManager) cleanupAWSDisks(ctx context.Context) error {
	err := wait.PollUntilContextTimeout(ctx, m.cleanupPollingInterval, m.cleanupTimeout, true, func(ctx context.Context) (bool, error) {
		volumes, err := m.getAWSTestVolumes(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to list AWS volumes for cleanup (deletion): %+v", err)
		}
		for _, volume := range volumes {
			if volume.State == "" {
				m.log.Info("volume did not have a state", "id", volume.VolumeId)
				return false, nil
			}

			if volume.State == ec2types.VolumeStateInUse {
				m.log.Info("detaching AWS Volume", "size", volume.Size, "id", volume.VolumeId)
				if _, err := m.ec2.DetachVolume(ctx, &ec2.DetachVolumeInput{VolumeId: volume.VolumeId}); err != nil {
					m.log.Error(err, "could not detach volume")
				}
				return false, nil
			}

			if volume.State != ec2types.VolumeStateAvailable {
				m.log.Info("waiting for volume to become available after detach", "desiredState", ec2types.VolumeStateAvailable, "currentState", volume.State, "id", volume.VolumeId)
				return false, nil
			}

			m.log.Info("deleting AWS Volume", "size", volume.Size, "id", volume.VolumeId)
			if _, err := m.ec2.DeleteVolume(ctx, &ec2.DeleteVolumeInput{VolumeId: volume.VolumeId}); err != nil {
				m.log.Error(err, "could not delete volume")
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

func (m *AWSDiskManager) getAWSTestVolumes(ctx context.Context) ([]ec2types.Volume, error) {
	output, err := m.ec2.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:purpose"),
				Values: []string{awsPurposeTag},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return output.Volumes, nil
}
