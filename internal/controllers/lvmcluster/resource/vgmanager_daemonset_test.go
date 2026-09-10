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

package resource

import (
	"testing"

	"github.com/openshift/lvm-operator/v4/internal/cluster"
	corev1 "k8s.io/api/core/v1"
)

// The node plugin needs the host SELinux policy store to handle the "-o context" mount
// option, and must not be able to write to it.
func TestTemplateVGManagerDaemonset_MountsSELinuxReadOnly(t *testing.T) {
	ds := templateVGManagerDaemonset(testCluster(), cluster.TypeOCP, "default", "test-image", nil, nil)

	var vol *corev1.Volume
	for i := range ds.Spec.Template.Spec.Volumes {
		if ds.Spec.Template.Spec.Volumes[i].Name == SELinuxVolName {
			vol = &ds.Spec.Template.Spec.Volumes[i]
		}
	}
	if vol == nil {
		t.Fatalf("no volume named %q on the daemonset", SELinuxVolName)
	}
	if vol.HostPath == nil {
		t.Fatalf("volume %q is not a hostPath volume", SELinuxVolName)
	}
	if vol.HostPath.Path != selinuxPath {
		t.Errorf("volume %q points at %q, want %q", SELinuxVolName, vol.HostPath.Path, selinuxPath)
	}

	if len(ds.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(ds.Spec.Template.Spec.Containers))
	}

	var mount *corev1.VolumeMount
	for i, m := range ds.Spec.Template.Spec.Containers[0].VolumeMounts {
		if m.Name == SELinuxVolName {
			mount = &ds.Spec.Template.Spec.Containers[0].VolumeMounts[i]
		}
	}
	if mount == nil {
		t.Fatalf("volume %q is declared but not mounted into the container", SELinuxVolName)
	}
	if mount.MountPath != selinuxPath {
		t.Errorf("mount path is %q, want %q", mount.MountPath, selinuxPath)
	}
	if !mount.ReadOnly {
		t.Errorf("mount of %q must be read-only", selinuxPath)
	}
}
