package e2e

import (
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	k8sv1 "k8s.io/api/core/v1"
)

func getPVC(pvcYAML string) (*k8sv1.PersistentVolumeClaim, error) {
	obj, _, err := deserializer.Decode([]byte(pvcYAML), nil, nil)
	if err != nil {
		return nil, err
	}
	pvc := obj.(*k8sv1.PersistentVolumeClaim)
	return pvc, nil
}

func getPod(podYAML string) (*k8sv1.Pod, error) {
	obj, _, err := deserializer.Decode([]byte(podYAML), nil, nil)
	if err != nil {
		return nil, err
	}
	pod := obj.(*k8sv1.Pod)
	return pod, nil
}

func getVolumeSnapshot(volumeSnapshotYAML string) (*snapapi.VolumeSnapshot, error) {
	obj, _, err := deserializer.Decode([]byte(volumeSnapshotYAML), nil, nil)
	if err != nil {
		return nil, err
	}
	volumeSnapshot := obj.(*snapapi.VolumeSnapshot)
	return volumeSnapshot, nil
}
