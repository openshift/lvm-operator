(import 'config.libsonnet') +
(import 'alerts/vgalerts.libsonnet') +
(import 'alerts/raidalerts.libsonnet') + {
  prometheus+:: {
        apiVersion: 'monitoring.coreos.com/v1',
        kind: 'PrometheusRule',
        metadata: {
          name: 'prometheus-lvmo-rules',
        },
        spec: {
          groups: $.prometheusAlerts.groups,
        },
  }
}
