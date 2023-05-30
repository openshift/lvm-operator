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
