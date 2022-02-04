{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'vg-alert.rules',
        rules: [
          {
            alert: 'VolumeGroupUsageAtThresholdNearFull',
            expr: |||
              (topolvm_volumegroup_size_bytes - topolvm_volumegroup_available_bytes)/topolvm_volumegroup_size_bytes > %(vgUsageThresholdNearFull)0.2f and (topolvm_volumegroup_size_bytes - topolvm_volumegroup_available_bytes)/topolvm_volumegroup_size_bytes <= %(vgUsageThresholdCritical)0.2f
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
              (topolvm_volumegroup_size_bytes - topolvm_volumegroup_available_bytes)/topolvm_volumegroup_size_bytes > %(vgUsageThresholdCritical)0.2f
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
    ],
  },
}
