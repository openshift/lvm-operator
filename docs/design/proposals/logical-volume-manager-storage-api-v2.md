---
title: logical-volume-manager-storage-api-v2
authors:
  - @jakobmoellerdev
reviewers: # Include a comment about what domain expertise a reviewer is expected to bring and what area of the enhancement you expect them to focus on. For example: - "@networkguru, for networking aspects, please look at IP bootstrapping aspect"
  - @brandisher, LVM Operator team manager
  - @suleymanakbas91
  - @jeff-roche
approvers: # A single approver is preferred, the role of the approver is to raise important questions, help ensure the enhancement receives reviews from all applicable areas/SMEs, and determine when consensus is achieved such that the EP can move forward to implementation.  Having multiple approvers makes it difficult to determine who is responsible for the actual approval.
  - @jerpeter1, LVM Operator staff engineer
  - @suleymanakbas91
  - @jeff-roche
api-approvers: # In case of new or modified APIs or API extensions (CRDs, aggregated apiservers, webhooks, finalizers). If there is no API change, use "None"
  - @suleymanakbas91
  - @jeff-roche
creation-date: 2023-09-22
last-updated: 2023-09-22
tracking-link: # link to the tracking ticket (for example: Jira Feature or Epic ticket) that corresponds to this enhancement
  - [OCPVE-662](https://issues.redhat.com/browse/OCPVE-662)
  - [OCPVE-703](https://issues.redhat.com/browse/OCPVE-703)
see-also:
  - N/A
replaces:
  - N/A
superseded-by:
  - N/A
---

# Logical Volume Manager Storage API v2

## Summary

This document is an Enhancement Proposal for implementing a new API version for the Logical Volume Manager Storage.
The objective of the proposal is to eliminate the assumptions made for single-node development, enabling greater configurability and ease of debugging.
It introduces changes such as the use of a Set and template which are similar to DaemonSet and Deployment respectively from Kubernetes.
The status aggregation and API interaction have been extensively rewritten.
Volume groups now can be reused by different device classes.
A transition from v1alpha1 to v2alpha1 will be provided with detailed guides, and v1alpha1 will be actively maintained until the new API is considered stable.
Risks involved include possible side effects from the adaptation and potential bugs from the major rewrite, but the authors propose that these can be mitigated through thorough documentation and extended active support of the old API version.

## Motivation

This enhancement proposal presents a comprehensive plan to develop the subsequent version of this API, with the goal of eliminating the limitations originated from assumptions made for single-node development.
Our aim is to converge our practices towards true best-practice API conventions adhering to upstream standards.
The proposed changes are designed to facilitate future development by permitting additional non-breaking changes.
This, in turn, should expedite the iteration process for developers aiming to report extra status updates.
Furthermore, we aim to simplify the process of debugging and collecting issues when encountering problems with LVMS, which currently lacks a streamlined reporting system. Hence, this proposal ultimately seeks to mitigate such issues and improve the overall user experience.

### User Stories

* As an OpenShift engineer, I want to be able to quickly debug issues with the Logical Volume Manager Storage (LVMS) without having to analyze separate resources for the desired and actual state. This means that the system should clearly display the difference between the expected and current system states, which would allow me to identify and resolve any discrepancies more efficiently.
* As an OpenShift engineer, I aim to comply with established Kubernetes API Specifications. Adhering to these conventions will simplify the process of onboarding other developers, as they're likely familiar with them, and aligns our company's practices with the wider developer community. This also means I can rely on the well-maintained upstream conventions, avoiding the need to reinvent the wheel.
* As an OpenShift engineer, I want the flexibility to design non-breaking API changes, allowing a faster iteration period on smaller API changes. This would enable iterative development and a more agile workflow, letting me incrementally build and improve our systems without causing system-wide disruptions.
* As a user of the LVMS API, I want all pertinent data for Volume Groups and Nodes readily available in one location. Having a centralized place for all related information will improve usability and make the process of managing storage configurations more straightforward and efficient.
* As a LVMS API user, I want to utilize my preexisting knowledge of Kubernetes APIs to understand the LVMS APIs more quickly. Leveraging knowledge I already have will reduce the learning curve and allow me to work more effectively and efficiently.
* As an OpenShift cluster administrator, I need to limit the minimum permissions of LVMS to namespace-level as much as possible. By doing so, I can mitigate the risk of privilege escalation into other namespaces, enhancing the overall security of the system. This would help me maintain the integrity and confidentiality of data across different namespaces, providing peace of mind and aiding in regulatory compliance.
* As an OpenShift cluster administrator, I wish to have the flexibility to deploy multiple configurations of the Logical Volume Manager Storage (LVMS) tailored to different use cases.
  This implies that the system should provide the capability to setup and manage distinct LVMS configurations concurrently. Such a feature would be beneficial in scenarios where different applications or services have diverse storage needs or performance requirements.


### Goals

* Change LVMS APIs to be compliant to the [official Kubernetes API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
* Change LVMS APIs to be compliant to [KEP-1623: Standardize Conditions](https://github.com/kubernetes/enhancements/blob/master/keps/sig-api-machinery/1623-standardize-conditions/README.md)
* Change LVMS APIs to be compliant with [Cluster/Namespaced Operator-Scope definitions](https://sdk.operatorframework.io/docs/building-operators/golang/operator-scope/#overview)
* Introduce new interfaces for developers to more accurately report statuses to LVMS API users

### Non-Goals

* Determine when and how `v1alpha1` gets deprecated or removed. We will have to supply enough information to allow transitioning to `v2alpha1` but should not immediately get rid of `v1alpha1`.

## Proposal

This is where we get down to the nitty gritty of what the proposal
actually is. Describe clearly what will be changed, including all of
the components that need to be modified and how they will be
different. Include the reason for each choice in the design and
implementation that is proposed here, and expand on reasons for not
choosing alternatives in the Alternatives section at the end of the
document.

### Workflow Description

Explain how the user will use the feature. Be detailed and explicit.
Describe all of the actors, their roles, and the APIs or interfaces
involved. Define a starting state and then list the steps that the
user would need to go through to trigger the feature described in the
enhancement. Optionally add a
[mermaid](https://github.com/mermaid-js/mermaid#readme) sequence
diagram.

Use sub-sections to explain variations, such as for error handling,
failure recovery, or alternative outcomes.

For example:

**cluster creator** is a human user responsible for deploying a
cluster.

**application administrator** is a human user responsible for
deploying an application in a cluster.

1. The cluster creator sits down at their keyboard...
2. ...
3. The cluster creator sees that their cluster is ready to receive
   applications, and gives the application administrator their
   credentials.

#### Variation [optional]

If the cluster creator uses a standing desk, in step 1 above they can
stand instead of sitting down.

See
https://github.com/openshift/enhancements/blob/master/enhancements/workload-partitioning/management-workload-partitioning.md#high-level-end-to-end-workflow
and https://github.com/openshift/enhancements/blob/master/enhancements/agent-installer/automated-workflow-for-agent-based-installer.md for more detailed examples.

### API Extensions


#### LVMCluster

The current LVMCluster will be broken up into node-specific configurations

```yaml
---
apiVersion: lvm.topolvm.io/v2alpha1
kind: LVMNodeSet
metadata:
  name: my-lvmnodeset
  namespace: openshift-storage
  uid: 31167fec-f8d3-47af-883f-05aaa153da88
spec:
  nodeSelector:
    nodeSelectorTerms:
      - matchExpressions:
          - key: topology.kubernetes.io/zone
            operator: In
            values:
              - antarctica-east1
              - antarctica-west1
      - matchExpressions:
          - key: custom.topology.domain.io/storage
            operator: In
            values:
              - archive-storage
  provider:
    strategy: ManagedTopoLVM
  template:
    volumeGroups:
      - name: vg1
        deviceSelector:
          deviceSelectorTerms:
            - matchExpressions:
                - key: kname
                  operator: In
                  values:
                    - "/dev/mapper/some-encrypted-device"
                    - "/dev/mapper/some-fallback-device"
            - matchExpressions:
                - key: kname
                  operator: In
                  values:
                    - "/dev/mapper/some-default-device"
      - name: othervg
        deviceSelector:
          deviceSelectorTerms:
            - matchExpressions:
                - key: kname
                  operator: Any
    deviceClasses:
      - name: thinly-provisioned
        strategy: Thin
        volumeGroup: vg1
        default: true
        thin:
          poolName: thin-pool-1
          sizePercent: 90
          overprovisionRatio: 10
        storageClass:
          namingPolicy: Prefixed
          prefix: "lvms"
        snapshotClass:
          namingPolicy: Prefixed
          prefix: "lvms"
status:
  state: Ready
  nodes:
    - name: archive-storage-node-in-antarctica-east1
      state: Ready
  provider:
    state: Ready
  conditions:
    - lastTransitionTime: "2044-10-22T16:29:24Z"
      status: "True"
      type: LVMNodesReady
    - lastTransitionTime: "2044-10-22T16:29:24Z"
      status: "True"
      type: ProviderReady
```

As one can see there are a few key distinctions compared to the old API:

1. The usage of a `Set` akin to `DaemonSet`. This is used to align the different `Node`'s into one single configurable object.
2. The introduction of a `template`, similar to a `Deployment`. This template will be used by all nodes in the `LVMNodeSet`.
3. Generic Conditions for checking if the LVMNodeSetup for all nodes is ready.
4. Separation of `deviceClasses` from `volumeGroups`. This is because VolumeGroups in theory can be reused by different deviceClasses
5. Alignment of the deviceSelector to use `deviceSelectorTerms` akin to `nodeSelectorTerms` to make configuration more straight-forward for users familiar with kubernetes APIs
6. `Thin` strategy within `deviceClasses` to allow Thick provisioning extension later on
7. `storageClass` settings within `deviceClasses` to allow custom naming and alignment of `StorageClass`
8. `snapshotClass` settings within `deviceClasses` to allow custom naming and alignment of `SnapshotClass`
9. `provider` introduces customizability of the TopoLVM resources and LVMD socket used within TopoLVM later on.
10. The split-up also allows to deploy multiple `LVMNodeSet` in the future, allowing us to stop prohibiting the creation of only a single `NodeSet`


```yaml
---
apiVersion: lvm.topolvm.io/v2alpha1
kind: LVMNode
metadata:
  name: archive-storage-node-in-antarctica-east1 # name of v1/Node
  namespace: openshift-storage
  ownerReferences:
    - apiVersion: apps/v1
      blockOwnerDeletion: true
      controller: true
      kind: LVMNodeSet
      name: my-lvmnodeset
      uid: 31167fec-f8d3-47af-883f-05aaa153da88
spec:
  volumeGroups:
    - name: vg1
      deviceSelector:
        deviceSelectorTerms:
          - matchExpressions:
              - key: kname
                operator: In
                values:
                  - "/dev/mapper/some-encrypted-device"
                  - "/dev/mapper/some-fallback-device"
          - matchExpressions:
              - key: kname
                operator: In
                values:
                  - "/dev/mapper/some-default-device"
    - name: othervg
      deviceSelector:
        deviceSelectorTerms:
          - matchExpressions:
              - key: kname
                operator: Any
  deviceClasses:
    - name: thinly-provisioned
      strategy: Thin
      volumeGroup: vg1
      default: true
      thin:
        poolName: thin-pool-1
        sizePercent: 90
        overprovisionRatio: 10
      storageClass:
        namingPolicy: Prefixed
        prefix: "lvms"
      snapshotClass:
        namingPolicy: Prefixed
        prefix: "lvms"
  lvmd:
    socketName: "/run/topolvm/lvmd.sock"
status:
  state: Ready
  volumeGroups:
    - name: vg1
      size: "2Gi"
      state: Healthy
      devices:
        included:
          - kname: "/dev/mapper/some-fallback-device"
          - kname: "/dev/mapper/some-default-device"
        excluded:
          - kname: "/dev/mapper/some-encrypted-device"
            reasons:
              - "/dev/mapper/some-fallback-device was found and used instead"
              - "/dev/mapper/some-encrypted-device was not found"
      volumes:
        - name: "thin-pool-1"
          metadataPercent: "30.0"
          attributes: "twi-a-tz--"
    - name: othervg
      state: Healthy
      size: "10Gi"
      devices:
        included:
          - kname: "/dev/sda2"
        excluded:
          - kname: "/dev/sda1"
            reasons:
              - "/dev/sda1 was used as a boot partition and cannot be used"
          - kname: "/dev/mapper/some-default-device"
            reasons:
              - "/dev/mapper/some-default-device is already being used by vg1 and cannot be used by a wildcard (Any) volumeGroup"
          - kname: "/dev/mapper/some-default-device"
            reasons:
              - "/dev/mapper/some-default-device is already being used by vg1 and cannot be used by a wildcard (Any) volumeGroup"
  deviceClasses:
    - name: thinly-provisioned
      volumeGroup: vg1
      state: Healthy
      storageClass:
        name: "lvms-vg1"
        state: Exists
      snapshotClass:
        name: "lvms-vg1"
        state: Exists
  conditions:
    - lastTransitionTime: "2044-10-22T16:29:24Z"
      status: "True"
      type: VolumeGroupsHealthy
    - lastTransitionTime: "2044-10-22T16:29:24Z"
      status: "True"
      type: DeviceClassesHealthy
    - lastTransitionTime: "2044-10-22T16:29:24Z"
      status: "True"
      type: StorageClassesHealthy
    - lastTransitionTime: "2044-10-22T16:29:24Z"
      status: "True"
      type: LVMDConfigured
```

Each individual node receives the entirety of the `LVMNodeSet` configuration in it's spec. However, compared to the NodeSet,
it contains detailed information about `status` elements from `volumeGroups` and `deviceClasses`, where each lvm volume group as well
as the contents of provisioned deviceClass are checked for consistency and are reported in status. In additions to this,
the Node contains more detailed conditions that allow setting up detailed status checks.

### Implementation Details/Notes/Constraints [optional]

What are the caveats to the implementation? What are some important details that
didn't come across above. Go in to as much detail as necessary here. This might
be a good place to talk about core concepts and how they relate.

### Risks and Mitigations

This will surely have a large impact on usage of LVMS. There is a high probability of rewriting a lot of the important
aspects of LVMS and it basically is an entirely rewritten codepath for the status aggregation and API interaction.

We will mitigate this by introducing detailed guides on how to migrate old configurations into new configurations on v2 and
will keep v1 active for as long as necessary (until we feel confident in the new API).

There will be additional risks discovered as we flesh out this proposal, but the biggest risk lies in bug introduction
in the new code path due to the size of the change as well as adoption side-effects.

### Drawbacks

1. **Complexity**: The proposed enhancement involves substantial changes to the existing API. Users familiar with the current API would need to adapt and learn the new system, which might be a hurdle for adoption. The system's overall complexity may also increase, potentially deterring some users.
2. **Transition Challenges**: Transitioning from the current version to the new one could pose some challenges. Although the team intends to provide detailed guides and maintain the old API until the new one is stable, there is a risk of unnoticed bugs or compatibility issues. As with any update, users could potentially lose data or face disruptions during the transition phase.
3. **Development and Maintenance Effort**: This enhancement could require substantial development effort to establish the new API and refactor the existing codes. Additionally, the maintenance burden could increase, especially if the team needs to support both the old and new versions simultaneously for some period of time.
4. **Potential Bugs**: With major rewrites, the probability of introducing bugs is high. Even though the new API is intended to provide clear status reporting and error handling, unexpected issues could arise, impacting the user experience or causing other problems.

## Design Details

### Open Questions [optional]

This is where to call out areas of the design that require closure before deciding
to implement the design.  For instance,
> 1. This requires exposing previously private resources which contain sensitive
     information.  Can we do this?

### Test Plan

The test plan will cover various aspects, including unit tests, integration and end-to-end tests.

We'll use both automated testing (to catch regressions and common use cases) and manual testing (to uncover less common scenarios).

1. **Unit Tests**: These will focus on functions and methods within the code. They will include tests for both the new API Versions as well as the controllers required. Special focus will be laid on additional API fields and defaulting behavior introduced by the new API.
2. **Integration Tests**: These will concern the interaction between different parts of the system.
   - Test the coordination between the LVMS APIs and the Kubernetes API conventions, KEP-1623, and the Operator-Scope definitions.
   - Test interfaces for developers to report statuses to LVMS API users.
     End-to-end Tests
3. **End-to-End Tests**: These will involve testing the system as a whole within Openshift, from installation and setup of LVMS to the creation and management of the logical volume groups and device classes. We will replicate every single scenario within the current API.

### Graduation Criteria

**Note:** *Section not required until targeted at a release.*

Define graduation milestones.

These may be defined in terms of API maturity, or as something else. Initial proposal
should keep this high-level with a focus on what signals will be looked at to
determine graduation.

Consider the following in developing the graduation criteria for this
enhancement:

- Maturity levels
    - [`alpha`, `beta`, `stable` in upstream Kubernetes][maturity-levels]
    - `Dev Preview`, `Tech Preview`, `GA` in OpenShift
- [Deprecation policy][deprecation-policy]

Clearly define what graduation means by either linking to the [API doc definition](https://kubernetes.io/docs/concepts/overview/kubernetes-api/#api-versioning),
or by redefining what graduation means.

In general, we try to use the same stages (alpha, beta, GA), regardless how the functionality is accessed.

[maturity-levels]: https://git.k8s.io/community/contributors/devel/sig-architecture/api_changes.md#alpha-beta-and-stable-versions
[deprecation-policy]: https://kubernetes.io/docs/reference/using-api/deprecation-policy/

**If this is a user facing change requiring new or updated documentation in [openshift-docs](https://github.com/openshift/openshift-docs/),
please be sure to include in the graduation criteria.**

**Examples**: These are generalized examples to consider, in addition
to the aforementioned [maturity levels][maturity-levels].

#### Dev Preview -> Tech Preview

- Ability to utilize the enhancement end to end
- End user documentation, relative API stability
- Sufficient test coverage
- Gather feedback from users rather than just developers
- Enumerate service level indicators (SLIs), expose SLIs as metrics
- Write symptoms-based alerts for the component(s)

#### Tech Preview -> GA

- More testing (upgrade, downgrade, scale)
- Sufficient time for feedback
- Available by default
- Backhaul SLI telemetry
- Document SLOs for the component
- Conduct load testing
- User facing documentation created in [openshift-docs](https://github.com/openshift/openshift-docs/)

**For non-optional features moving to GA, the graduation criteria must include
end to end tests.**

#### Removing a deprecated feature

- Announce deprecation and support policy of the existing feature
- Deprecate the feature

### Upgrade / Downgrade Strategy

If applicable, how will the component be upgraded and downgraded? Make sure this
is in the test plan.

Consider the following in developing an upgrade/downgrade strategy for this
enhancement:
- What changes (in invocations, configurations, API use, etc.) is an existing
  cluster required to make on upgrade in order to keep previous behavior?
- What changes (in invocations, configurations, API use, etc.) is an existing
  cluster required to make on upgrade in order to make use of the enhancement?

Upgrade expectations:
- Each component should remain available for user requests and
  workloads during upgrades. Ensure the components leverage best practices in handling [voluntary
  disruption](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/). Any exception to
  this should be identified and discussed here.
- Micro version upgrades - users should be able to skip forward versions within a
  minor release stream without being required to pass through intermediate
  versions - i.e. `x.y.N->x.y.N+2` should work without requiring `x.y.N->x.y.N+1`
  as an intermediate step.
- Minor version upgrades - you only need to support `x.N->x.N+1` upgrade
  steps. So, for example, it is acceptable to require a user running 4.3 to
  upgrade to 4.5 with a `4.3->4.4` step followed by a `4.4->4.5` step.
- While an upgrade is in progress, new component versions should
  continue to operate correctly in concert with older component
  versions (aka "version skew"). For example, if a node is down, and
  an operator is rolling out a daemonset, the old and new daemonset
  pods must continue to work correctly even while the cluster remains
  in this partially upgraded state for some time.

Downgrade expectations:
- If an `N->N+1` upgrade fails mid-way through, or if the `N+1` cluster is
  misbehaving, it should be possible for the user to rollback to `N`. It is
  acceptable to require some documented manual steps in order to fully restore
  the downgraded cluster to its previous state. Examples of acceptable steps
  include:
    - Deleting any CVO-managed resources added by the new version. The
      CVO does not currently delete resources that no longer exist in
      the target version.

### Version Skew Strategy

How will the component handle version skew with other components?
What are the guarantees? Make sure this is in the test plan.

Consider the following in developing a version skew strategy for this
enhancement:
- During an upgrade, we will always have skew among components, how will this impact your work?
- Does this enhancement involve coordinating behavior in the control plane and
  in the kubelet? How does an n-2 kubelet without this feature available behave
  when this feature is used?
- Will any other components on the node change? For example, changes to CSI, CRI
  or CNI may require updating that component before the kubelet.

### Operational Aspects of API Extensions

Describe the impact of API extensions (mentioned in the proposal section, i.e. CRDs,
admission and conversion webhooks, aggregated API servers, finalizers) here in detail,
especially how they impact the OCP system architecture and operational aspects.

- For conversion/admission webhooks and aggregated apiservers: what are the SLIs (Service Level
  Indicators) an administrator or support can use to determine the health of the API extensions

  Examples (metrics, alerts, operator conditions)
    - authentication-operator condition `APIServerDegraded=False`
    - authentication-operator condition `APIServerAvailable=True`
    - openshift-authentication/oauth-apiserver deployment and pods health

- What impact do these API extensions have on existing SLIs (e.g. scalability, API throughput,
  API availability)

  Examples:
    - Adds 1s to every pod update in the system, slowing down pod scheduling by 5s on average.
    - Fails creation of ConfigMap in the system when the webhook is not available.
    - Adds a dependency on the SDN service network for all resources, risking API availability in case
      of SDN issues.
    - Expected use-cases require less than 1000 instances of the CRD, not impacting
      general API throughput.

- How is the impact on existing SLIs to be measured and when (e.g. every release by QE, or
  automatically in CI) and by whom (e.g. perf team; name the responsible person and let them review
  this enhancement)

#### Failure Modes

- Describe the possible failure modes of the API extensions.
- Describe how a failure or behaviour of the extension will impact the overall cluster health
  (e.g. which kube-controller-manager functionality will stop working), especially regarding
  stability, availability, performance and security.
- Describe which OCP teams are likely to be called upon in case of escalation with one of the failure modes
  and add them as reviewers to this enhancement.

#### Support Procedures

Describe how to
- detect the failure modes in a support situation, describe possible symptoms (events, metrics,
  alerts, which log output in which component)

  Examples:
    - If the webhook is not running, kube-apiserver logs will show errors like "failed to call admission webhook xyz".
    - Operator X will degrade with message "Failed to launch webhook server" and reason "WehhookServerFailed".
    - The metric `webhook_admission_duration_seconds("openpolicyagent-admission", "mutating", "put", "false")`
      will show >1s latency and alert `WebhookAdmissionLatencyHigh` will fire.

- disable the API extension (e.g. remove MutatingWebhookConfiguration `xyz`, remove APIService `foo`)

    - What consequences does it have on the cluster health?

      Examples:
        - Garbage collection in kube-controller-manager will stop working.
        - Quota will be wrongly computed.
        - Disabling/removing the CRD is not possible without removing the CR instances. Customer will lose data.
          Disabling the conversion webhook will break garbage collection.

    - What consequences does it have on existing, running workloads?

      Examples:
        - New namespaces won't get the finalizer "xyz" and hence might leak resource X
          when deleted.
        - SDN pod-to-pod routing will stop updating, potentially breaking pod-to-pod
          communication after some minutes.

    - What consequences does it have for newly created workloads?

      Examples:
        - New pods in namespace with Istio support will not get sidecars injected, breaking
          their networking.

- Does functionality fail gracefully and will work resume when re-enabled without risking
  consistency?

  Examples:
    - The mutating admission webhook "xyz" has FailPolicy=Ignore and hence
      will not block the creation or updates on objects when it fails. When the
      webhook comes back online, there is a controller reconciling all objects, applying
      labels that were not applied during admission webhook downtime.
    - Namespaces deletion will not delete all objects in etcd, leading to zombie
      objects when another namespace with the same name is created.

## Implementation History

Major milestones in the life cycle of a proposal should be tracked in `Implementation
History`.

## Alternatives

Similar to the `Drawbacks` section the `Alternatives` section is used to
highlight and record other possible approaches to delivering the value proposed
by an enhancement.

## Infrastructure Needed [optional]

Use this section if you need things from the project. Examples include a new
subproject, repos requested, github details, and/or testing infrastructure.

Listing these here allows the community to get the process for these resources
started right away.
