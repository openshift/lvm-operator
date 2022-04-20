package e2e

import (
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetSamplePVC returns a sample pvc.
func GetSamplePVC(storageClass, quantity, name string, volumemode k8sv1.PersistentVolumeMode) *k8sv1.PersistentVolumeClaim {
	pvc := &k8sv1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: TestNamespace,
		},
		Spec: k8sv1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes:      []k8sv1.PersistentVolumeAccessMode{k8sv1.ReadWriteOnce},
			VolumeMode:       &volumemode,
			Resources: k8sv1.ResourceRequirements{
				Requests: k8sv1.ResourceList{
					k8sv1.ResourceStorage: resource.MustParse(quantity),
				},
			},
		},
	}
	return pvc
}

// GetSamplePod returns a sample pod.
func GetSamplePod(name, pvcName string) *k8sv1.Pod {
	pod := &k8sv1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: TestNamespace,
		},
		Spec: k8sv1.PodSpec{
			Volumes: []k8sv1.Volume{
				{
					Name: "storage",
					VolumeSource: k8sv1.VolumeSource{
						PersistentVolumeClaim: &k8sv1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
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
				},
			},
		},
	}
	return pod
}
