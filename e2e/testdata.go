package e2e

import (
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var snapAPIGroup = "snapshot.storage.k8s.io"

// getSamplePod returns a sample pod.
func getSamplePod(name, pvcName string) *k8sv1.Pod {
	pod := &k8sv1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
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

// getSampleVolumeSnapshot creates and returns the VolumeSnapshot for the provided PVC.
func getSampleVolumeSnapshot(snapshotName, sourceName string, storageClass string) *snapapi.VolumeSnapshot {
	vs := &snapapi.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      snapshotName,
			Namespace: testNamespace,
		},
		Spec: snapapi.VolumeSnapshotSpec{
			VolumeSnapshotClassName: &storageClass,
			Source: snapapi.VolumeSnapshotSource{
				PersistentVolumeClaimName: &sourceName,
			},
		},
	}
	return vs
}

// getSamplePvc returns restore or clone of the pvc based on the kind provided.
func getSamplePvc(size, name string, volumemode k8sv1.PersistentVolumeMode, storageClass string, sourceType, sourceName string) *k8sv1.PersistentVolumeClaim {
	pvc := &k8sv1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: k8sv1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes:      []k8sv1.PersistentVolumeAccessMode{k8sv1.ReadWriteOnce},
			VolumeMode:       &volumemode,
			Resources: k8sv1.ResourceRequirements{
				Requests: k8sv1.ResourceList{
					k8sv1.ResourceStorage: resource.MustParse(size),
				},
			},
		},
	}
	if sourceType != "" && sourceName != "" {
		if sourceType == "VolumeSnapshot" {
			pvc.Spec.DataSource = &k8sv1.TypedLocalObjectReference{
				Name:     sourceName,
				Kind:     sourceType,
				APIGroup: &snapAPIGroup,
			}
		} else {
			pvc.Spec.DataSource = &k8sv1.TypedLocalObjectReference{
				Name: sourceName,
				Kind: sourceType,
			}
		}

	}
	return pvc
}
