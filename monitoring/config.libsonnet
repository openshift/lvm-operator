{
  _config+:: {
    // volumegroup usage percentage threshold near full
    vgUsageThresholdNearFull: 0.75,

    // volumegroup usage percentage threshold critical
    vgUsageThresholdCritical: 0.85,

    // alert durations
    volumegroupUsageThresholdAlertTime: '5m',

    // thin pool data and metadata usage percentage threshold near full
    thinPoolUsageThresholdNearFull : 75,

    // thin pool data and metadata usage percentage threshold critical
    thinPoolUsageThresholdCritical : 85,

    // alert durations
    thinPoolUsageThresholdAlertTime: '5m',

    // RAID alert durations
    raidDegradedAlertTime: '1m',
    raidFailedAlertTime: '1m',
    raidSyncSlowAlertTime: '30m',
  },
}
