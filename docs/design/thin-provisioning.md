# Thin Provisioning

## Summary
- LVM thin provisioning allows the creation of volumes whose combined virtual size is greater than that of the available storage.

**Advantages**:
- Storage space can be used more effectively. More users can be accommodated for the same amount of storage space when compared to thick provisioning. This significantly reduces upfront hardware cost for the storage admins.
- Faster clones and snapshots

**Disadvantages** :
- Reduced performance when compared to thick volumes.
- Over-allocation of the space. This can be mitigated by better monitoring of the disk usage.

LVM does this by allocating the blocks in a thin LV from a special "thin pool LV". A thin pool LV must be created before thin LVs can be created within it.

The LVMS will create a thin pool LV in the Volume Group in order to create thinly provisioned volumes.

## Design Details

- The `deviceClass` API in the `LVMClusterSpec` contains the mapping between a device-class and a thin-pool in volume group.
- One device-class is mapped to a single thin pool.
- Users can configure the thin pool size based on percentage of the available Volume Group size.
- Default chunk size of the thin pool is 128 kiB.
- `lvmd.yaml` config file is updated with the device class, volume group and thin-pool mapping.
- Alerts are triggered if the thin-pool `data` or `metadata` usage crosses a predefined threshold limit.

### API

- `LVMClusterSpec.Storage.DeviceClass.ThinPoolConfig` has the mapping between device class, volume group and the thin-pool.
- One DeviceClass can be mapped to only one thin-pool.

- `LVMCluster` API changes:
    ```go
    + type ThinPoolConfig struct{
    +   // Name of the thin pool to be created
    +   // +kubebuilder:validation:Required
    +   // +required
    +   Name string `json:"name,omitempty"`
    
    +   // SizePercent represents percentage of remaining space in the volume group that should be used
    +   // for creating the thin pool.
    +   // +kubebuilder:validation:default=90
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

- The following new fields are added to `DeviceClass` API
    - **ThinPoolConfig** API contains information related to a thin pool.These configuration options are:
        - **Name**: Name of the thin-pool
        - **SizePercent**: Size of the thin pool to be created with respect to available free space in the volume group. It represents percentage value and not absolute size values. Size value should range between 10-90. It defaults to 90 if no value is provided.
        - **OverprovisionRatio**: The factor by which additional storage can be provisioned compared to the available storage in the thin pool.

- `LVMVolumeGroup` API changes:
    
    ```go
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

### Volume Group Manager
- [Volume Group Manager](vg-manager.md) is responsible for creating the thin pools after creating the volume group.
- Command used for creating a thin pool:
    
    ```bash
    lvcreate -L <Size>%FREE -c <Chunk size> -T <vg_name>/<thin-pool-name>
    ```
  
    where:
    - Size is `LVMClusterSpec.Storage.DeviceClass.ThinPoolConfig.SizePercent`
    - chunk size is 128KiB, which is the default.

- VG manager also updates the `lvmd.yaml` file to map Volume Group and its thin pool to the TopoLVM device class.
- Sample `lvmd.yaml` config file

```yaml
device-classes:
  - name: ssd-thin
    volume-group: myvg1
    spare-gb: 10
    type: thin
    thin-pool-config:
        name: pool0
        overprovision-ratio: 5.0
```

### Monitoring and Alerts
- Available thin pool size (both data and metadata) is provided by TopoLVM as prometheus metrics.
- Threshold limits for the thin pool are provided as static values in the PrometheusRule.
- If the data or metadata usage for a particular thin-pool crosses a threshold, appropriate alerts are triggered.
