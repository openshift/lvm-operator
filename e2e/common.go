package e2e

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
)

//nolint:errcheck
func debug(msg string, args ...interface{}) {
	ginkgo.GinkgoWriter.Write([]byte(fmt.Sprintf(msg, args...)))
}

// GetSamplePVC returns a sample pvc.
func GetSamplePVC(storageClass string, quantity string) *k8sv1.PersistentVolumeClaim {
	storageQuantity, err := resource.ParseQuantity(quantity)
	gomega.Expect(err).To(gomega.BeNil())

	pvc := &k8sv1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lvmpvc",
			Namespace: TestNamespace,
		},
		Spec: k8sv1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes:      []k8sv1.PersistentVolumeAccessMode{k8sv1.ReadWriteOnce},

			Resources: k8sv1.ResourceRequirements{
				Requests: k8sv1.ResourceList{
					"storage": storageQuantity,
				},
			},
		},
	}

	return pvc
}

// GetSamplePod returns a sample pod.
func GetSamplePod() *k8sv1.Pod {
	pod := &k8sv1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lvmpod",
			Namespace: TestNamespace,
		},
		Spec: k8sv1.PodSpec{
			Volumes: []k8sv1.Volume{
				{
					Name: "storage",
					VolumeSource: k8sv1.VolumeSource{
						PersistentVolumeClaim: &k8sv1.PersistentVolumeClaimVolumeSource{
							ClaimName: "lvmpvc",
						},
					},
				},
			},
			Containers: []k8sv1.Container{
				{
					Name:  "container",
					Image: "public.ecr.aws/docker/library/nginx:latest",
					Ports: []k8sv1.ContainerPort{
						{
							Name:          "http-server",
							ContainerPort: 80,
						},
					},
					VolumeMounts: []k8sv1.VolumeMount{
						{
							MountPath: "/usr/share/nginx/html",
							Name:      "storage",
						},
					},
				},
			},
		},
	}
	return pod
}

// WaitForPVCBound waits for a pvc with a given name and namespace to reach BOUND phase.
func WaitForPVCBound(pvc *k8sv1.PersistentVolumeClaim, namespace string, pod *k8sv1.Pod) error {
	pvc, err := DeployManagerObj.GetK8sClient().CoreV1().PersistentVolumeClaims(namespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	pod, err = DeployManagerObj.GetK8sClient().CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	lastReason := ""
	timeout := 100 * time.Second
	interval := 1 * time.Second

	// Wait for namespace to terminate
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		pvc, err = DeployManagerObj.GetK8sClient().CoreV1().PersistentVolumeClaims(pvc.Namespace).Get(context.TODO(), pvc.Name, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			lastReason = fmt.Sprintf("error talking to k8s apiserver: %v", err)
			return false, nil
		}

		if pvc.Status.Phase != k8sv1.ClaimBound {
			lastReason = fmt.Sprintf("waiting on pvc %s/%s to reach bound state, currently %s", pvc.Namespace, pvc.Name, pvc.Status.Phase)
			return false, nil
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("%v: %s", err, lastReason)
	}

	return nil
}
