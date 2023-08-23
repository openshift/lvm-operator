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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func diskSetup(ctx context.Context) error {
	// get nodes
	By(fmt.Sprintf("getting all worker nodes by label %s", labelNodeRoleWorker))
	nodeList := &corev1.NodeList{}
	if err := crClient.List(ctx, nodeList, client.HasLabels{labelNodeRoleWorker}); err != nil {
		return fmt.Errorf("could not list worker nodes nodes for Disk setup: %w", err)
	}

	By("getting AWS region info from the first Node spec")
	nodeInfo, err := getAWSNodeInfo(nodeList.Items[0])
	Expect(err).NotTo(HaveOccurred(), "getAWSNodeInfo")

	// initialize client
	By("initializing ec2 client with the previously attained region")
	ec2, err := getEC2Client(ctx, nodeInfo.Region)
	Expect(err).NotTo(HaveOccurred(), "getEC2Client")

	// represents the Disk layout to setup on the nodes.
	nodeEnv, err := getNodeEnvironmentFromNodeList(nodeList)
	Expect(err).NotTo(HaveOccurred(), "getNodeEnvironmentFromNodeList")

	// create and attach volumes
	By("creating and attaching Disks")
	Expect(NewAWSDiskManager(ec2, GinkgoLogr).CreateAndAttachAWSVolumes(ctx, nodeEnv)).To(Succeed())

	return nil
}

func getNodeEnvironmentFromNodeList(nodeList *corev1.NodeList) ([]NodeDisks, error) {
	var nodeEnv []NodeDisks
	for _, node := range nodeList.Items {
		nodeInfo, err := getAWSNodeInfo(node)
		if err != nil {
			return nil, fmt.Errorf("could not get node environment: %w", err)
		}
		disks := NodeDisks{
			Node: node.GetName(),
			Disks: []Disk{
				{Size: 10},
				{Size: 20},
			},
			AWSNodeInfo: nodeInfo,
		}
		GinkgoLogr.Info("preparing Node", "Node", node.GetName(), "Disks", disks)
		nodeEnv = append(nodeEnv, disks)
	}
	return nodeEnv, nil
}

func diskRemoval(ctx context.Context) error {
	// get nodes
	By(fmt.Sprintf("getting all worker nodes by label %s", labelNodeRoleWorker))
	nodeList := &corev1.NodeList{}
	if err := crClient.List(ctx, nodeList, client.HasLabels{labelNodeRoleWorker}); err != nil {
		return fmt.Errorf("could not list worker nodes nodes for Disk setup: %w", err)
	}

	By("getting AWS region info from the first Node spec")
	nodeInfo, err := getAWSNodeInfo(nodeList.Items[0])
	Expect(err).NotTo(HaveOccurred(), "getAWSNodeInfo")

	// initialize client
	By("initializing ec2 client with the previously attained region")
	ec2, err := getEC2Client(ctx, nodeInfo.Region)
	Expect(err).NotTo(HaveOccurred(), "getEC2Client")

	// cleaning Disk
	By("cleaning up Disks")
	Expect(NewAWSDiskManager(ec2, GinkgoLogr).cleanupAWSDisks(ctx)).To(Succeed())

	return err
}
