# LVMO: Thin provisioning

## Summary
- LVM thin provisioning allows creation of volumes whose combined size is greater than that of the available storage.

**Advantages**:
- Storage space can be used more effectively. More users can be accommodated for the same amount of storage space when compared to thick provisioning. This significantly reduces upfront hardware cost for the storage admins.
- Faster clones and snapshots

**Disadvantages** :
- Reduced performance when compared to thick volumes.
- Over-allocation of the space. This can be mitigated by better monitoring of the disk usage.

LVM does this by allocating the blocks in a thin LV from a special "thin pool LV". A thin pool LV must be created before thin LVs can be created within it.

The LVMO will create a thin-pool LV in the volume group in order to create thinly provisioned volumes.


## Proposal:
- The `deviceClass` API in the `LVMClusterSpec` will contain the mapping between a device-class and a thin-pool in volume group.
- One device-class will be mapped to a single thin pool.
- User should be able to configure the thin-pool size based on percentage of the available volume group size.
- Default chunk size of the thin pool will be 512 kiB
- `lvmd.yaml` config file should be updated with the device class, volume group and thin-pool mapping.
- Alerts should be triggered if a thin-pool `data` or `metadata` available size crosses a predefined threshold limit.


## Design Details
### API changes:

- `LVMClusterSpec.Storage.DeviceClass.ThinPoolConfig` will have the mapping between device class, volume group and the thin-pool.
- One DeviceClass can be mapped to only one thin-pool.

- `LVMCluster` API changes
```go=
+ type ThinPoolConfig struct{
+   // Name of the thin pool to be created
+   // +kubebuilder:validation:Required
+   // +required
+   Name string `json:"name,omitempty"`

+   // SizePercent represents percentage of remaining space in the volume group that should be used
+   // for creating the thin pool.
+   // +kubebuilder:validation:default=75
+   // +kubebuilder:validation:Minimum=10
+   // +kubebuilder:validation:Maximum=90
+   SizePercent int `json:"sizePercent,omitempty"`

+   // OverProvisionRatio represents the ratio of overprovision that can
+   // be allowed on thin pools
+   // +kubebuilder:validation:Minimum=1
+   OverprovisionRatio int `json:"overprovisionRatio,omitempty"`
}

type DeviceClass struct {
    Name string `json:"name,omitempty"`

    DeviceSelector *DeviceSelector `json:"deviceSelector,omitempty"`
    NodeSelector *corev1.NodeSelector `json:"nodeSelector,omitempty"`

+   // ThinPoolConfig contains configurations for the thin-pool
+   // +kubebuilder:validation:Required
+   // +required
+   ThinPoolConfig  *ThinPoolConfig `json:"thinPoolConfig,omitempty"`
}
```


- Following new fields will added to `DeviceClass` API
    - **ThinPoolConfig** API contains information related to a thin pool.These configuration options are:
        - **Name**: Name of the thin-pool
        - **SizePercent**: Size of the thin pool to be created with respect to available free space in the volume group. It represents percentage value and not absolute size values. Size value should range between 10-90. It defaults to 75 if no value is provided.
        - **OverprovisionRatio**: The factor by which additional storage can be provisioned compared to the available storage in the thin pool.

- `LVMVolumeGroup` API changes:

``` go=
type LVMVolumeGroupSpec struct {
    // DeviceSelector is a set of rules that should match for a device to be
    // included in this VolumeGroup
    // +optional
    DeviceSelector *DeviceSelector `json:"deviceSelector,omitempty"`

    // NodeSelector chooses nodes
    // +optional
    NodeSelector *corev1.NodeSelector `json:"nodeSelector,omitempty"`

+   // ThinPoolConfig contains configurations for the thin-pool
+   // +kubebuilder:validation:Required
+   // +required
+   ThinPoolConfig *ThinPoolConfig `json:"thinPoolConfig,omitempty"`
}
```

### VolumeGroup Manager
- Volume Group Manager is responsible for creating the thin-pools after creating the volume group.
- Command used for creating the thin-pool:
    ```
    lvcreate -L <Size>%FREE -c <Chunk size> -T <vg_name>/<thin-pool-name>
    ```
    where:
    - Size is `LVMClusterSpec.Storage.DeviceClass.ThinPoolConfig.SizePercent`
    - chunk size is 512KiB, which is the default.

- VG manager will also update the `lvmd.yaml` file to map volume group and its thin-pool to the topolvm device class.
- Sample `lvmd.yaml` config file
``` yaml=
device-classes:
  - name: ssd-thin
    volume-group: myvg1
    spare-gb: 10
    type: thin
    thin-pool-config:
        name: pool0
        overprovision-ratio: 50.0
```

### Monitoring and Alerts
- Available thin-pool size (both data and metadata) should be provided by topolvm as prometheus metrics.
- Threshold limits for the thin-pool should be provide as static values in the PrometheusRule.
- If used size of data or metadata for a particular thin-pool crosses the threshold, then appropriate alerts should be triggered.


### Open questions
- What should be the chunk size of the thin-pools?
    - Use default size a 512 kiB for now.
