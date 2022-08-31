/*
Copyright 2022 Red Hat Openshift Data Foundation.

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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

type nodeDisks struct {
	disks []disk
	node  corev1.Node
}

type disk struct {
	size int
}

const (
	awsPurposeTag       = "odf-lvmo"
	labelNodeRoleWorker = "node-role.kubernetes.io/worker"
)

// getAWSNodeInfo returns instanceID, region, zone, error
func getAWSNodeInfo(node corev1.Node) (string, string, string, error) {
	var instanceID, region, zone string
	// providerID looks like: aws:///us-east-2a/i-02d314dea14ed4efb
	if !strings.HasPrefix(node.Spec.ProviderID, "aws://") {
		return "", "", "", fmt.Errorf("not an aws based node")
	}
	split := strings.Split(node.Spec.ProviderID, "/")
	instanceID = split[len(split)-1]
	zone = split[len(split)-2]
	region = zone[:len(zone)-1]
	return instanceID, region, zone, nil
}

// this assumes that the device spaces /dev/sd[h-z] are available on the node
// do not provide more than 20 disksize
// do not use more than once per node
// this function is async
func createAndAttachAWSVolumes(ec2Client *ec2.EC2, nodeEnv []nodeDisks) error {
	for _, nodeEntry := range nodeEnv {
		err := createAndAttachAWSVolumesForNode(nodeEntry, ec2Client)
		if err != nil {
			return err
		}
	}
	return nil
}

func createAndAttachAWSVolumesForNode(nodeEntry nodeDisks, ec2Client *ec2.EC2) error {
	node := nodeEntry.node
	volumes := make([]*ec2.Volume, len(nodeEntry.disks))
	volumeLetters := []string{"g", "h"}
	volumeIDs := make([]*string, 0)
	instanceID, _, zone, err := getAWSNodeInfo(node)
	if err != nil {
		fmt.Printf("failed to create and attach aws disks for node %q\n", nodeEntry.node.Name)
		return err
	}

	// create ec2 volumes
	for i, disk := range nodeEntry.disks {
		diskSize := disk.size
		diskName := fmt.Sprintf("sd%s", volumeLetters[i])
		createInput := &ec2.CreateVolumeInput{
			AvailabilityZone: aws.String(zone),
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
							Value: aws.String(instanceID),
						},
					},
				},
			},
		}
		volume, err := ec2Client.CreateVolume(createInput)
		if err != nil {
			err := fmt.Errorf("expect to create AWS volume with input %v: %w", createInput, err)
			fmt.Printf("failed to create and attach aws disks for node %q\n", nodeEntry.node.Name)
			return err
		}
		fmt.Printf("creating volume: %q (%dGi)\n", *volume.VolumeId, *volume.Size)
		volumes[i] = volume
		volumeIDs = append(volumeIDs, volume.VolumeId)
	}
	// attach and poll for attachment to complete
	err = wait.Poll(time.Second*5, time.Minute*4, func() (bool, error) {
		describeVolumeInput := &ec2.DescribeVolumesInput{
			VolumeIds: volumeIDs,
		}
		describedVolumes, err := ec2Client.DescribeVolumes(describeVolumeInput)
		if err != nil {
			return false, err
		}
		allAttached := true
		for i, volume := range describedVolumes.Volumes {
			if *volume.State == ec2.VolumeStateInUse {
				fmt.Printf("volume attachment complete: %q (%dGi)\n", *volume.VolumeId, *volume.Size)
				continue
			}
			allAttached = false
			if *volume.State == ec2.VolumeStateAvailable {

				fmt.Printf("volume attachment starting: %q (%dGi)\n", *volume.VolumeId, *volume.Size)
				attachInput := &ec2.AttachVolumeInput{
					VolumeId:   volume.VolumeId,
					InstanceId: aws.String(instanceID),
					Device:     aws.String(fmt.Sprintf("/dev/sd%s", volumeLetters[i])),
				}
				_, err = ec2Client.AttachVolume(attachInput)
				if err != nil {
					return false, err
				}
			}
		}
		return allAttached, nil

	})
	if err != nil {
		fmt.Printf("failed to create and attach aws disks for node %q\n", nodeEntry.node.Name)
		return err
	}
	return nil
}

func getEC2Client(ctx context.Context, region string) (*ec2.EC2, error) {
	// get AWS credentials
	awsCreds := &corev1.Secret{}
	secretName := types.NamespacedName{Name: "aws-creds", Namespace: "kube-system"}
	err := crClient.Get(ctx, secretName, awsCreds)
	if err != nil {
		return nil, err
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
	})
	if err != nil {
		return nil, err
	}

	// initialize client
	return ec2.New(sess), nil
}

func cleanupAWSDisks(ec2Client *ec2.EC2) error {
	volumes, err := getAWSTestVolumes(ec2Client)
	if err != nil {
		fmt.Printf("failed to list AWS volumes")
		return err
	}
	for _, volume := range volumes {
		fmt.Printf("detaching AWS disks with volumeId: %q (%dGi)\n", *volume.VolumeId, *volume.Size)
		input := &ec2.DetachVolumeInput{VolumeId: volume.VolumeId}
		_, err := ec2Client.DetachVolume(input)
		if err != nil {
			fmt.Printf("detaching disk failed")
			return err
		}
	}
	err = wait.Poll(time.Second*2, time.Minute*5, func() (bool, error) {
		volumes, err := getAWSTestVolumes(ec2Client)
		if err != nil {
			return false, fmt.Errorf("failed to list AWS volumes: %+v", err)
		}
		allDeleted := true
		for _, volume := range volumes {
			if *volume.State != ec2.VolumeStateAvailable {
				fmt.Printf("volume %q is in state %q, waiting for state %q\n", *volume.VolumeId, *volume.State, ec2.VolumeStateAvailable)
				allDeleted = false
				continue
			}
			fmt.Printf("deleting AWS disks with volumeId: %q (%dGi)\n", *volume.VolumeId, *volume.Size)
			input := &ec2.DeleteVolumeInput{VolumeId: volume.VolumeId}
			_, err := ec2Client.DeleteVolume(input)
			if err != nil {
				fmt.Printf("deleting disk failed: %+v\n", err)
				allDeleted = false
			}
		}
		return allDeleted, nil
	})
	if err != nil {
		fmt.Printf("Failed AWS cleanup of disks")
		return err
	}
	return nil
}

func getAWSTestVolumes(ec2Client *ec2.EC2) ([]*ec2.Volume, error) {
	output, err := ec2Client.DescribeVolumes(&ec2.DescribeVolumesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:purpose"),
				Values: []*string{aws.String(awsPurposeTag)},
			},
		},
	})

	return output.Volumes, err

}
