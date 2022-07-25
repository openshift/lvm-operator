package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func diskSetup(ctx context.Context) error {

	// get nodes
	nodeList := &corev1.NodeList{}
	err := crClient.List(ctx, nodeList, client.HasLabels{labelNodeRoleWorker})
	if err != nil {
		fmt.Printf("failed to list nodes\n")
		return err
	}

	fmt.Printf("getting AWS region info from node spec\n")
	_, region, _, err := getAWSNodeInfo(nodeList.Items[0])
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "getAWSNodeInfo")

	// initialize client
	fmt.Printf("initialize ec2 creds\n")
	ec2Client, err := getEC2Client(ctx, region)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "getEC2Client")

	// represents the disk layout to setup on the nodes.
	var nodeEnv []nodeDisks
	for _, node := range nodeList.Items {
		nodeEnv = append(nodeEnv, nodeDisks{

			disks: []disk{
				{size: 10},
				{size: 20},
			},
			node: node,
		})
	}

	// create and attach volumes
	fmt.Printf("creating and attaching disks\n")
	err = createAndAttachAWSVolumes(ec2Client, nodeEnv)
	gomega.Expect(err).To(gomega.BeNil())

	return nil
}

func diskRemoval(ctx context.Context) error {
	// get nodes
	nodeList := &corev1.NodeList{}
	err := crClient.List(ctx, nodeList, client.HasLabels{labelNodeRoleWorker})
	if err != nil {
		fmt.Printf("failed to list nodes\n")
		return err
	}
	fmt.Printf("getting AWS region info from node spec\n")
	_, region, _, err := getAWSNodeInfo(nodeList.Items[0])
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "getAWSNodeInfo")

	// initialize client
	fmt.Printf("initialize ec2 creds\n")
	ec2Client, err := getEC2Client(ctx, region)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "getEC2Client")

	// cleaning disk
	fmt.Printf("cleaning up disks\n")
	err = cleanupAWSDisks(ec2Client)
	gomega.Expect(err).To(gomega.BeNil())

	return err
}
