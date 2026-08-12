{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'raid-alert.rules',
        rules: [
          {
            alert: 'LVMSRAIDDegraded',
            expr: |||
              lvms_raid_health_status == 1
            |||,
            'for': $._config.raidDegradedAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              description: "RAID array in device class {{ $labels.device_class }} on node {{ $labels.node }} is degraded. One or more devices may have failed. Run 'lvs -o+sync_percent,lv_health_status' on the node to inspect and consult the LVMS documentation for manual repair steps.",
              message: 'RAID array {{ $labels.device_class }} is degraded on node {{ $labels.node }}.',
            },
          },
          {
            alert: 'LVMSRAIDFailed',
            expr: |||
              lvms_raid_health_status == 2
            |||,
            'for': $._config.raidFailedAlertTime,
            labels: {
              severity: 'critical',
            },
            annotations: {
              description: "RAID array in device class {{ $labels.device_class }} on node {{ $labels.node }} has failed. Data may be at risk. Immediate manual intervention is required. Run 'lvs -o+sync_percent,lv_health_status' on the node and consult the LVMS documentation for recovery steps.",
              message: 'RAID array {{ $labels.device_class }} has failed on node {{ $labels.node }}.',
            },
          },
          {
            alert: 'LVMSRAIDSyncSlow',
            expr: |||
              lvms_raid_sync_in_progress == 1
            |||,
            'for': $._config.raidSyncSlowAlertTime,
            labels: {
              severity: 'warning',
            },
            annotations: {
              description: "RAID resynchronization in device class {{ $labels.device_class }} on node {{ $labels.node }} has been running for more than " + $._config.raidSyncSlowAlertTime + ". This may indicate a slow or failing device. Run 'lvs -o+sync_percent,raid_sync_action' on the node to check progress.",
              message: 'RAID sync in {{ $labels.device_class }} on node {{ $labels.node }} is taking longer than expected.',
            },
          },
        ],
      },
    ],
  },
}
