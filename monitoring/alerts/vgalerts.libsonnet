{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'vg-alert.rules',
        rules: [
          {
            alert: 'VolumeGroupUsageAtThresholdNearFull',
            expr: |||
              (topolvm_volumegroup_size_bytes - topolvm_volumegroup_available_bytes)/topolvm_volumegroup_size_bytes > %(vgUsageThresholdNearFull)0.2f and (topolvm_volumegroup_size_bytes - topolvm_volumegroup_available_bytes)/topolvm_volumegroup_size_bytes <= %(vgUsageThresholdCritical)0.2f and topolvm_thinpool_data_percent > %(thinPoolUsageThresholdNearFull)0.2f  and topolvm_thinpool_data_percent < %(thinPoolUsageThresholdCritical)0.2f
            ||| % $._config,
            'for': $._config.volumegroupUsageThresholdAlertTime,
            labels: {
              severity: 'warning',
            },
            annotations: {
              description: 'VolumeGroup is nearing full. Data deletion or VolumeGroup expansion is required.',
              message: 'VolumeGroup {{ $labels.device_class }} utilization has crossed %.0f %% on node {{ $labels.node }}. Free up some space or expand the VolumeGroup.' % ($._config.vgUsageThresholdNearFull*100),
            },
          },
          {
            alert: 'VolumeGroupUsageAtThresholdCritical',
            expr: |||
              (topolvm_volumegroup_size_bytes - topolvm_volumegroup_available_bytes)/topolvm_volumegroup_size_bytes > %(vgUsageThresholdCritical)0.2f and topolvm_thinpool_data_percent > %(thinPoolUsageThresholdCritical)0.2f
            ||| % $._config,
            'for': $._config.volumegroupUsageThresholdAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              description: 'VolumeGroup is critically full. Data deletion or VolumeGroup expansion is required.',
              message: 'VolumeGroup {{ $labels.device_class }} utilization has crossed %.0f %% on node {{ $labels.node }}. Free up some space or expand the VolumeGroup immediately.' % ($._config.vgUsageThresholdCritical * 100),
            },
          },
	],
      },
      {
        name: 'thin-pool-alert.rules',
        rules: [
          {
            alert: 'ThinPoolDataUsageAtThresholdNearFull',
            expr: |||
              topolvm_thinpool_data_percent > %(thinPoolUsageThresholdNearFull)0.2f  and topolvm_thinpool_data_percent < %(thinPoolUsageThresholdCritical)0.2f
            ||| % $._config,
            'for': $._config.thinPoolUsageThresholdAlertTime,
            labels: {
              severity: 'warning',
            },
            annotations: {
              description: 'Thin pool in the VolumeGroup is nearing full. Data deletion or thin pool expansion is required.',
              message: 'Thin Pool data utilization in the VolumeGroup {{ $labels.device_class }} has crossed %.0f %% on node {{ $labels.node }}. Free up some space or expand the thin pool.' % ($._config.thinPoolUsageThresholdNearFull),
            },
          },
          {
            alert: 'ThinPoolDataUsageAtThresholdCritical',
            expr: |||
              topolvm_thinpool_data_percent > %(thinPoolUsageThresholdCritical)0.2f
            ||| % $._config,
            'for': $._config.thinPoolUsageThresholdAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              description: 'Thin pool in the VolumeGroup is critically full. Data deletion or thin pool expansion is required.',
              message: 'Thin Pool data utilization in the VolumeGroup {{ $labels.device_class }} has crossed %.0f %% on node {{ $labels.node }}. Free up some space or expand the thin pool immediately.' % ($._config.thinPoolUsageThresholdCritical),
            },
          },
          {
            alert: 'ThinPoolMetaDataUsageAtThresholdNearFull',
            expr: |||
              topolvm_thinpool_metadata_percent > %(thinPoolUsageThresholdNearFull)0.2f  and topolvm_thinpool_data_percent < %(thinPoolUsageThresholdCritical)0.2f
            ||| % $._config,
            'for': $._config.thinPoolUsageThresholdAlertTime,
            labels: {
              severity: 'warning',
            },
            annotations: {
              description: 'Thin pool metadata utitlization in the VolumeGroup is nearing full. Data deletion or thin pool expansion is required.',
              message: 'Thin Pool metadata utilization in the VolumeGroup {{ $labels.device_class }} has crossed %.0f %% on node {{ $labels.node }}. Free up some space or expand the thin pool.' % ($._config.thinPoolUsageThresholdNearFull),
            },
          },
          {
            alert: 'ThinPoolMetaDataUsageAtThresholdCritical',
            expr: |||
              topolvm_thinpool_metadata_percent > %(thinPoolUsageThresholdCritical)0.2f
            ||| % $._config,
            'for': $._config.thinPoolUsageThresholdAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              description: 'Thin pool metadata ultilization in the VolumeGroup is critically full. Data deletion or thin pool expansion is required.',
              message: 'Thin Pool metadata utilization in the VolumeGroup {{ $labels.device_class }} has crossed %.0f %% on node {{ $labels.node }}. Free up some space or expand the thin pool immediately.' % ($._config.thinPoolUsageThresholdCritical),
            },
          },
	],
      },
    ],
  },
}
