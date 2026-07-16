# RAID Feature — Comprehensive Test Plan

Manual testing plan for the LVMS RAID feature on a live OpenShift cluster.
Tests simulate real user/admin workflows via `oc`/`kubectl` commands.

## Prerequisites

- OpenShift cluster with latest LVMS operator deployed
- `oc` CLI authenticated with cluster-admin
- One worker node with enough disk space for 10 x 1GB backing files (~10GB in /var/tmp)
- `dm-raid` kernel module available on the target node (default on RHEL/CoreOS)

## Environment Variables

Set these before running tests:

```bash
export NODE="<worker-node-name>"
export NS="openshift-storage"  # or wherever LVMS is deployed
```

---

## Phase 1: Setup

### 1.1 Select Target Node

```bash
oc get nodes -l node-role.kubernetes.io/worker --no-headers -o custom-columns=NAME:.metadata.name
```

Pick one node and set `NODE`.

### 1.2 Verify dm-raid Module

```bash
oc debug node/$NODE -- chroot /host modinfo dm-raid
```

**Expected**: Module info printed. If missing, RAID tests cannot proceed.

### 1.3 Create 10 x 1GB Loop Devices

```bash
oc debug node/$NODE -- chroot /host bash -c '
  for i in $(seq 0 9); do
    dd if=/dev/zero of=/var/tmp/raid-test-loop${i}.img bs=1M count=1024 status=none
    LOOP=$(losetup --find --show /var/tmp/raid-test-loop${i}.img)
    echo "Created: $LOOP -> /var/tmp/raid-test-loop${i}.img"
  done
'
```

### 1.4 Record Loop Device Paths

```bash
oc debug node/$NODE -- chroot /host bash -c 'losetup -l -J' | jq -r '.loopdevices[] | select(.["back-file"] | test("raid-test")) | .name'
```

Store these as `LOOP0` through `LOOP9`. All subsequent tests reference these paths.

### 1.5 Verify Devices Are Visible

```bash
oc debug node/$NODE -- chroot /host lsblk --list --noheadings -o NAME,SIZE,TYPE | grep loop
```

**Expected**: 10 loop devices, each 1GB.

---

## Phase 2: Happy Path — Every RAID Level and Configuration Field

Each test follows this pattern:
1. Apply LVMCluster CR
2. Wait for Ready status
3. Create PVC + Pod, write test data
4. Verify LVM state on node (`lvs`, `vgs`, `pvs`)
5. Verify LVMVolumeGroupNodeStatus CR (raidStatus)
6. Verify Prometheus metrics
7. Clean up (delete Pod, PVC, LVMCluster, wait for teardown)

### Verification Commands (reused across tests)

```bash
# Check LVMCluster status
oc get lvmclusters -n $NS -o yaml

# Check node status CR
oc get lvmvolumegroupnodestatuses -n $NS -o yaml

# Check LVM state on node
oc debug node/$NODE -- chroot /host vgs --noheadings
oc debug node/$NODE -- chroot /host pvs --noheadings
oc debug node/$NODE -- chroot /host lvs -o lv_name,vg_name,lv_attr,lv_size,lv_layout,raid_sync_percent,seg_type,stripes,stripe_size --noheadings

# Check metrics (from vg-manager pod)
VG_POD=$(oc get pods -n $NS -l app.kubernetes.io/name=vg-manager --field-selector spec.nodeName=$NODE -o jsonpath='{.items[0].metadata.name}')
oc exec -n $NS $VG_POD -- curl -s http://localhost:8080/metrics | grep lvms_raid

# Check StorageClass
oc get sc -o yaml | grep -A5 "lvms"

# Check no VolumeSnapshotClass for RAID DCs
oc get volumesnapshotclasses
```

### Test 2.1: raid1 — Default Configuration

```yaml
apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: raid-test
spec:
  storage:
    deviceClasses:
    - name: raid1-default
      fstype: ext4
      default: true
      nodeSelector:
        nodeSelectorTerms:
        - matchExpressions:
          - key: kubernetes.io/hostname
            operator: In
            values: [$NODE]
      deviceSelector:
        paths:
        - $LOOP0
        - $LOOP1
      raidConfig:
        type: raid1
```

**Verify:**
- `lvs` shows LV with `lv_layout` containing `raid,raid1`, `seg_type` = `raid1`
- `pvs` shows 2 PVs in the VG
- Node status: `raidStatus.status: Healthy`, `memberCount: 2`, `degradedMemberCount: 0`
- Metrics: `lvms_raid_health_status=0`, `lvms_raid_member_count=2`, `lvms_raid_degraded_count=0`, `lvms_raid_sync_percent=100`, `lvms_raid_sync_in_progress=0`
- No VolumeSnapshotClass created for this DC
- StorageClass exists, uses thick provisioning

**PVC + Pod test:**

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: raid-test-pvc
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 100Mi
  storageClassName: lvms-raid1-default
---
apiVersion: v1
kind: Pod
metadata:
  name: raid-test-pod
  labels:
    app: raid-test
spec:
  nodeName: $NODE
  containers:
  - name: test
    image: registry.access.redhat.com/ubi9/ubi-minimal:latest
    command: ["sh", "-c", "echo 'RAID_TEST_DATA_12345' > /data/testfile && md5sum /data/testfile > /data/checksum && sleep 3600"]
    volumeMounts:
    - name: data
      mountPath: /data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: raid-test-pvc
```

**Verify data write**: `oc exec raid-test-pod -- cat /data/testfile` returns `RAID_TEST_DATA_12345`

**Cleanup**: Delete pod, PVC, LVMCluster. Wait for finalizers. Verify VG removed on node.

### Test 2.2: raid1 — mirrors=2 (3-Way Mirror)

```yaml
raidConfig:
  type: raid1
  mirrors: 2
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2]
```

**Verify:**
- `lvs` shows `-m 2` geometry (3 copies)
- `pvs` shows 3 PVs
- `memberCount: 3`

### Test 2.3: raid4 — Default Configuration

```yaml
raidConfig:
  type: raid4
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2]
```

**Verify:**
- `lvs` shows `seg_type=raid4`
- 3 PVs (2 data + 1 dedicated parity)

### Test 2.4: raid4 — stripes=3 + stripeSize=128Ki

```yaml
raidConfig:
  type: raid4
  stripes: 3
  stripeSize: "128Ki"
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2, $LOOP3]
```

**Verify:**
- `lvs` shows `stripes=3`, `stripe_size=128.00k` (or equivalent)
- 4 PVs (3 data stripes + 1 parity)

### Test 2.5: raid5 — Default Configuration

```yaml
raidConfig:
  type: raid5
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2]
```

**Verify:**
- `lvs` shows `seg_type=raid5`
- 3 PVs (2 data + 1 distributed parity)

### Test 2.6: raid5 — stripes=3 + stripeSize=256Ki

```yaml
raidConfig:
  type: raid5
  stripes: 3
  stripeSize: "256Ki"
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2, $LOOP3]
```

**Verify:**
- `lvs` shows `stripes=3`, `stripe_size=256.00k`
- 4 PVs

### Test 2.7: raid6 — Default Configuration (5 Devices)

```yaml
raidConfig:
  type: raid6
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2, $LOOP3, $LOOP4]
```

**Verify:**
- `lvs` shows `seg_type=raid6`
- 5 PVs (3 data + 2 distributed parity)

### Test 2.8: raid6 — stripes=4 + stripeSize=64Ki

```yaml
raidConfig:
  type: raid6
  stripes: 4
  stripeSize: "64Ki"
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2, $LOOP3, $LOOP4, $LOOP5]
```

**Verify:**
- `lvs` shows `stripes=4`, `stripe_size=64.00k`
- 6 PVs

### Test 2.9: raid10 — Default Configuration (4 Devices)

```yaml
raidConfig:
  type: raid10
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2, $LOOP3]
```

**Verify:**
- `lvs` shows `seg_type=raid10`
- 4 PVs, geometry = 2 mirrors x 2 stripes

### Test 2.10: raid10 — mirrors=2 (6 Devices, 3-Copy Mirrors)

```yaml
raidConfig:
  type: raid10
  mirrors: 2
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2, $LOOP3, $LOOP4, $LOOP5]
```

**Verify:**
- 6 PVs, count divisible by 3 (mirrors+1)
- `memberCount: 6`

### Test 2.11: raid10 — All Fields (mirrors + stripes + stripeSize)

```yaml
raidConfig:
  type: raid10
  mirrors: 1
  stripes: 2
  stripeSize: "512Ki"
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2, $LOOP3]
```

**Verify:**
- `lvs` shows `stripe_size=512.00k`, `stripes=2`
- 4 PVs

### Test 2.12: raid1 ext4 — Data Integrity Across Pod Reschedule

1. Create raid1 + PVC + Pod
2. Write checksum: `oc exec raid-test-pod -- sh -c "dd if=/dev/urandom bs=1M count=10 of=/data/bigfile && md5sum /data/bigfile"`
3. Record checksum
4. Delete pod (not PVC)
5. Recreate pod with same PVC
6. Verify checksum: `oc exec raid-test-pod -- md5sum /data/bigfile`
7. **Expected**: Checksums match

---

## Phase 3: Validation Rejection — Bad Input Blocked

Apply each CR and verify rejection. No cluster state changes should occur.

```bash
# Pattern for each test:
oc apply -f bad-cr.yaml 2>&1
# Expected: error message containing the expected string
```

### Test 3.1: RAID + ThinPool Mutual Exclusion

```yaml
deviceClasses:
- name: bad-dc
  raidConfig:
    type: raid1
  thinPoolConfig:
    name: thin-pool-1
    sizePercent: 90
    overprovisionRatio: 10
  deviceSelector:
    paths: [$LOOP0, $LOOP1]
```

**Expected**: `raidConfig and thinPoolConfig are mutually exclusive`

### Test 3.2: Invalid RAID Type (raid0)

```yaml
raidConfig:
  type: raid0
```

**Expected**: CRD schema rejection (enum validation)

### Test 3.3: mirrors on raid5

```yaml
raidConfig:
  type: raid5
  mirrors: 1
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2]
```

**Expected**: `mirrors is only valid for raid1 and raid10`

### Test 3.4: mirrors on raid4

```yaml
raidConfig:
  type: raid4
  mirrors: 1
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2]
```

**Expected**: `mirrors is only valid for raid1 and raid10`

### Test 3.5: mirrors on raid6

```yaml
raidConfig:
  type: raid6
  mirrors: 1
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2, $LOOP3, $LOOP4]
```

**Expected**: `mirrors is only valid for raid1 and raid10`

### Test 3.6: stripes on raid1

```yaml
raidConfig:
  type: raid1
  stripes: 2
deviceSelector:
  paths: [$LOOP0, $LOOP1]
```

**Expected**: `stripes is only valid for raid4, raid5, raid6, and raid10`

### Test 3.7: stripeSize on raid1

```yaml
raidConfig:
  type: raid1
  stripeSize: "256Ki"
deviceSelector:
  paths: [$LOOP0, $LOOP1]
```

**Expected**: `stripeSize is only valid for raid4, raid5, raid6, and raid10`

### Test 3.8: stripeSize Not Power of 2

```yaml
raidConfig:
  type: raid5
  stripeSize: "100Ki"
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2]
```

**Expected**: `stripeSize must be a power of 2`

### Test 3.9: Too Few Devices — raid1 (need 2, give 1)

```yaml
raidConfig:
  type: raid1
deviceSelector:
  paths: [$LOOP0]
```

**Expected**: `requires at least 2 devices, got 1`

### Test 3.10: Too Few Devices — raid5 (need 3, give 2)

```yaml
raidConfig:
  type: raid5
deviceSelector:
  paths: [$LOOP0, $LOOP1]
```

**Expected**: `requires at least 3 devices, got 2`

### Test 3.11: Too Few Devices — raid6 (need 5, give 4)

```yaml
raidConfig:
  type: raid6
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2, $LOOP3]
```

**Expected**: `requires at least 5 devices, got 4`

### Test 3.12: Too Few Devices — raid10 (need 4, give 3)

```yaml
raidConfig:
  type: raid10
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2]
```

**Expected**: `requires at least 4 devices, got 3`

### Test 3.13: raid10 Non-Divisible Count (3 devices, mirrors=1, need even)

```yaml
raidConfig:
  type: raid10
  mirrors: 1
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2]
```

**Expected**: `requires device count to be a multiple of 2`

### Test 3.14: No Device Paths with RAID

```yaml
raidConfig:
  type: raid1
# no deviceSelector
```

**Expected**: `at least one of paths or optionalPaths is required when raidConfig is set`

### Test 3.15: Stripes Below Minimum (stripes=1)

```yaml
raidConfig:
  type: raid5
  stripes: 1
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2]
```

**Expected**: CRD schema rejection (Minimum=2)

### Test 3.16: Mirrors Below Minimum (mirrors=0)

```yaml
raidConfig:
  type: raid1
  mirrors: 0
deviceSelector:
  paths: [$LOOP0, $LOOP1]
```

**Expected**: CRD schema rejection (Minimum=1)

### Test 3.17: Invalid stripeSize Value (Zero)

```yaml
raidConfig:
  type: raid5
  stripeSize: "0"
deviceSelector:
  paths: [$LOOP0, $LOOP1, $LOOP2]
```

**Expected**: Rejected (stripeSize <= 0)

### Test 3.18: Invalid RAID Type (arbitrary string)

```yaml
raidConfig:
  type: raid99
```

**Expected**: CRD schema rejection (enum)

---

## Phase 4: Immutability — Updates Blocked

First, create a valid raid1 cluster and wait for Ready:

```yaml
apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: raid-immutable-test
spec:
  storage:
    deviceClasses:
    - name: raid1-dc
      fstype: ext4
      default: true
      nodeSelector:
        nodeSelectorTerms:
        - matchExpressions:
          - key: kubernetes.io/hostname
            operator: In
            values: [$NODE]
      deviceSelector:
        paths:
        - $LOOP0
        - $LOOP1
      raidConfig:
        type: raid1
```

Then attempt each mutation:

### Test 4.1: Change RAID Type (raid1 → raid5)

```bash
oc patch lvmcluster raid-immutable-test -n $NS --type=merge -p '
  {"spec":{"storage":{"deviceClasses":[{"name":"raid1-dc","raidConfig":{"type":"raid5"},"deviceSelector":{"paths":["'$LOOP0'","'$LOOP1'","'$LOOP2'"]}}]}}}'
```

**Expected**: `raidConfig is immutable after creation`

### Test 4.2: Change mirrors (1 → 2)

```bash
oc patch lvmcluster raid-immutable-test -n $NS --type=merge -p '
  {"spec":{"storage":{"deviceClasses":[{"name":"raid1-dc","raidConfig":{"type":"raid1","mirrors":2},"deviceSelector":{"paths":["'$LOOP0'","'$LOOP1'","'$LOOP2'"]}}]}}}'
```

**Expected**: `raidConfig is immutable after creation`

### Test 4.3: Add stripes to raid1

```bash
# Attempt to add stripes field
```

**Expected**: Rejected (immutability or invalid field for raid1)

### Test 4.4: Remove raidConfig Entirely

```bash
oc patch lvmcluster raid-immutable-test -n $NS --type=json -p '[{"op":"remove","path":"/spec/storage/deviceClasses/0/raidConfig"}]'
```

**Expected**: `raidConfig cannot be changed`

### Test 4.5: Add raidConfig to Non-RAID Device Class

1. Create a plain (non-RAID, non-thin) LVMCluster first
2. Then try to add raidConfig

**Expected**: `raidConfig cannot be changed`

### Test 4.6: Add Device Paths (ALLOWED)

```bash
oc patch lvmcluster raid-immutable-test -n $NS --type=json -p '[
  {"op":"replace","path":"/spec/storage/deviceClasses/0/deviceSelector/paths","value":["'$LOOP0'","'$LOOP1'","'$LOOP2'"]}
]'
```

**Expected**: Update succeeds. Third device added to VG. `memberCount` increases to 3.

**Cleanup**: Delete LVMCluster, wait for teardown.

---

## Phase 5: Day-2 Operations

### Test 5.1: Add Device to Existing RAID VG

1. Create raid1 with 2 devices, wait for Ready
2. Patch to add a 3rd device path
3. **Verify**: `pvs` shows 3 PVs, `memberCount` increases, VG size grows

### Test 5.2: PVC Expansion on RAID Storage

1. Create raid1 + 100Mi PVC + Pod
2. Expand PVC to 200Mi: `oc patch pvc raid-test-pvc -p '{"spec":{"resources":{"requests":{"storage":"200Mi"}}}}'`
3. Wait for PVC condition `FileSystemResizePending` → resize completes
4. **Verify**: `df -h` in pod shows larger filesystem, existing data intact

### Test 5.3: Multiple PVCs on Same RAID Device Class

1. Create raid1 with 2 devices
2. Create 3 PVCs (100Mi each) against same StorageClass
3. Create 3 pods, each mounting one PVC
4. Write different data to each
5. **Verify**: All 3 PVCs provisioned, all data readable, `lvs` shows 3 RAID LVs in same VG

### Test 5.4: Pod Reschedule — Data Persistence

1. Create raid1 + PVC + Pod, write data with checksum
2. `oc delete pod raid-test-pod`
3. Recreate identical pod
4. **Verify**: Data and checksum intact

### Test 5.5: OptionalPaths — Partial Device Availability

```yaml
deviceSelector:
  paths:
  - $LOOP0
  - $LOOP1
  optionalPaths:
  - /dev/loop99   # does not exist
  - $LOOP2        # exists
```

**Verify:**
- VG created with 3 devices (LOOP0, LOOP1, LOOP2)
- /dev/loop99 silently skipped
- `memberCount: 3`
- Status: Healthy

### Test 5.6: OptionalPaths — All Optional Missing But Minimum Met

```yaml
raidConfig:
  type: raid1
deviceSelector:
  paths:
  - $LOOP0
  - $LOOP1
  optionalPaths:
  - /dev/loop98   # does not exist
  - /dev/loop99   # does not exist
```

**Verify:**
- VG created with 2 devices (minimum met by paths alone)
- Status: Healthy

### Test 5.7: OptionalPaths — Minimum Not Met at Runtime

```yaml
raidConfig:
  type: raid5
deviceSelector:
  optionalPaths:
  - $LOOP0
  - /dev/loop98   # does not exist
  - /dev/loop99   # does not exist
```

**Verify:**
- Webhook accepts (3 optionalPaths >= 3 minimum)
- VG Manager rejects at runtime: only 1 device available, need 3
- Device class status: Failed

---

## Phase 6: Failure, Recovery & Observability

### Phase 6A: Baseline Metrics on Healthy RAID

**Setup**: Create raid1 with 2 devices + PVC.

### Test 6A.1: Healthy Metrics

```bash
VG_POD=$(oc get pods -n $NS -l app.kubernetes.io/name=vg-manager --field-selector spec.nodeName=$NODE -o jsonpath='{.items[0].metadata.name}')
oc exec -n $NS $VG_POD -- curl -s http://localhost:8080/metrics | grep lvms_raid
```

**Expected:**
```text
lvms_raid_health_status{device_class="raid1-dc",node="$NODE"} 0
lvms_raid_sync_in_progress{device_class="raid1-dc",node="$NODE"} 0
lvms_raid_member_count{device_class="raid1-dc",node="$NODE"} 2
lvms_raid_degraded_count{device_class="raid1-dc",node="$NODE"} 0
lvms_raid_sync_percent{device_class="raid1-dc",node="$NODE"} 100
```

### Test 6A.2: Healthy Status CR

```bash
oc get lvmvolumegroupnodestatuses -n $NS -o jsonpath='{.items[*].status}' | jq '.deviceClassStatuses[] | select(.name=="raid1-dc") | .raidStatus'
```

**Expected:**
```json
{
  "status": "Healthy",
  "memberCount": 2,
  "degradedMemberCount": 0,
  "minSyncPercent": 100,
  "lvHealth": [{"name": "<lv-name>", "raidType": "raid1", "syncPercent": 100}]
}
```

### Test 6A.3: VG Manager Logs — Health Monitoring Requeue

```bash
oc logs -n $NS $VG_POD --tail=50 | grep -i "requeue\|raid\|reconcil"
```

**Expected**: Logs show `RequeueAfter: 60s` for RAID device class.

### Test 6A.4: RAID Alerts Exist But Not Firing

```bash
oc get prometheusrules -n $NS -o yaml | grep -A5 "LVMSRAIDDegraded\|LVMSRAIDFailed\|LVMSRAIDSyncSlow"
```

**Expected**: All 3 alert rules present. None firing (check via `oc exec` into Prometheus or Thanos if available).

### Phase 6B: Single Device Failure — Degraded State

**Prerequisite**: Healthy raid1 from 6A with active PVC.

### Test 6B.1: Disconnect Loop Device

```bash
# Identify which loop device to disconnect
oc debug node/$NODE -- chroot /host pvs --noheadings
# Pick one PV (e.g., $LOOP1)
oc debug node/$NODE -- chroot /host losetup -d $LOOP1
```

### Test 6B.2: Wait for VG Manager Reconcile

```bash
# RAID reconcile interval is 60s — wait up to 90s
sleep 90
```

### Test 6B.3: Degraded Metrics

```bash
oc exec -n $NS $VG_POD -- curl -s http://localhost:8080/metrics | grep lvms_raid
```

**Expected:**
```text
lvms_raid_health_status{...} 1
lvms_raid_degraded_count{...} 1
lvms_raid_member_count{...} 2
```

### Test 6B.4: Degraded Status CR

```bash
oc get lvmvolumegroupnodestatuses -n $NS -o jsonpath='{.items[*].status}' | jq '.deviceClassStatuses[] | select(.name=="raid1-dc") | .raidStatus'
```

**Expected:** `status: Degraded`, `degradedMemberCount: 1`

### Test 6B.5: VGStatus Degraded

```bash
oc get lvmvolumegroupnodestatuses -n $NS -o jsonpath='{.items[*].status}' | jq '.deviceClassStatuses[] | select(.name=="raid1-dc") | .vgStatus'
```

**Expected:** `Degraded`

### Test 6B.6: Existing PVC Still Readable

```bash
oc exec raid-test-pod -- cat /data/testfile
```

**Expected**: Data intact — raid1 redundancy protects against single device loss.

### Test 6B.7: Write During Degraded State

```bash
oc exec raid-test-pod -- sh -c "echo 'WRITTEN_WHILE_DEGRADED' > /data/degraded-write && sync"
```

**Expected**: Write succeeds.

### Test 6B.8: New PVC During Degraded State

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: raid-degraded-pvc
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 50Mi
  storageClassName: lvms-raid1-dc
```

**Expected**: PVC provisions successfully (reduced redundancy).

### Test 6B.9: RAIDDegraded Alert

```bash
# Wait at least 1m (alert `for` threshold)
# Check alerts via Prometheus API or:
oc exec -n openshift-monitoring -c prometheus prometheus-k8s-0 -- \
  curl -s 'http://localhost:9090/api/v1/alerts' | jq '.data.alerts[] | select(.labels.alertname=="LVMSRAIDDegraded")'
```

**Expected**: `LVMSRAIDDegraded` alert firing with `severity: critical`.

### Test 6B.10: Kubernetes Events

```bash
oc get events -n $NS --field-selector reason=RAIDHealthCheckFailed
```

**Expected**: Warning event with message about missing physical volumes.

### Phase 6C: Recovery from Degraded

### Test 6C.1: Re-attach Loop Device

```bash
oc debug node/$NODE -- chroot /host bash -c "
  losetup $LOOP1 /var/tmp/raid-test-loop1.img
  echo \"Re-attached: $LOOP1\"
"
```

### Test 6C.2: LVM Repair

```bash
oc debug node/$NODE -- chroot /host bash -c "
  pvscan --cache
  vgextend --restoremissing <vg-name> $LOOP1
"
# Or if needed:
# lvconvert --repair <vg-name>/<lv-name>
```

### Test 6C.3: Sync In Progress

```bash
# Immediately check metrics
oc exec -n $NS $VG_POD -- curl -s http://localhost:8080/metrics | grep lvms_raid_sync
```

**Expected:** `lvms_raid_sync_in_progress=1`, `lvms_raid_sync_percent` < 100

### Test 6C.4: Sync Progress in Status CR

```bash
oc get lvmvolumegroupnodestatuses -n $NS -o jsonpath='{.items[*].status}' | jq '.deviceClassStatuses[] | select(.name=="raid1-dc") | .raidStatus.lvHealth'
```

**Expected**: `syncPercent` increasing over successive checks.

### Test 6C.5: Sync Completes

```bash
# Wait for sync (1GB devices sync quickly unless throttled)
oc debug node/$NODE -- chroot /host lvs -o lv_name,raid_sync_percent --noheadings
```

**Expected**: `raid_sync_percent` reaches 100.

### Test 6C.6: Healthy Status Restored

```bash
# After next reconcile (up to 60s after sync completes)
oc exec -n $NS $VG_POD -- curl -s http://localhost:8080/metrics | grep lvms_raid
```

**Expected:**
```text
lvms_raid_health_status{...} 0
lvms_raid_sync_in_progress{...} 0
lvms_raid_sync_percent{...} 100
lvms_raid_degraded_count{...} 0
```

### Test 6C.7: Alert Resolves

**Expected**: `LVMSRAIDDegraded` alert stops firing.

### Test 6C.8: Data Integrity

```bash
oc exec raid-test-pod -- cat /data/testfile
oc exec raid-test-pod -- cat /data/degraded-write
```

**Expected**: Both original and degraded-write data intact.

### Phase 6D: Total Failure — Both Devices Gone (raid1)

**Setup**: Fresh raid1 with 2 devices + PVC.

### Test 6D.1: Remove First Device

```bash
oc debug node/$NODE -- chroot /host losetup -d $LOOP0
```

Wait 90s for reconcile. Verify Degraded state (as in 6B).

### Test 6D.2: Remove Second Device

```bash
oc debug node/$NODE -- chroot /host losetup -d $LOOP1
```

### Test 6D.3: Failed Metrics

```bash
oc exec -n $NS $VG_POD -- curl -s http://localhost:8080/metrics | grep lvms_raid_health_status
```

**Expected:** `lvms_raid_health_status{...} 2`

### Test 6D.4: Failed Status CR

**Expected:** `raidStatus.status: Failed`, `degradedMemberCount: 2`

### Test 6D.5: RAIDFailed Alert

```bash
# Alert fires after 1m (faster than Degraded's 1m threshold)
oc exec -n openshift-monitoring -c prometheus prometheus-k8s-0 -- \
  curl -s 'http://localhost:9090/api/v1/alerts' | jq '.data.alerts[] | select(.labels.alertname=="LVMSRAIDFailed")'
```

**Expected**: `LVMSRAIDFailed` firing, severity=critical.

### Test 6D.6: PVC I/O Fails

```bash
oc exec raid-test-pod -- cat /data/testfile
```

**Expected**: I/O error (both copies lost).

### Phase 6E: Parity-Based Failure (raid5)

**Setup**: Create raid5 with 3 devices + PVC with data.

### Test 6E.1: Single Device Failure on raid5

```bash
oc debug node/$NODE -- chroot /host losetup -d $LOOP2
```

**Expected**: Degraded. Data still accessible (parity allows reconstruction).

```bash
oc exec raid-test-pod -- cat /data/testfile
```

**Expected**: Read succeeds.

### Test 6E.2: Second Device Failure on raid5

```bash
oc debug node/$NODE -- chroot /host losetup -d $LOOP1
```

**Expected**: Failed. raid5 cannot survive 2 device failures.

### Test 6E.3: Metric Progression

Track `lvms_raid_health_status` transitions: `0 → 1 → 2` as devices fail sequentially.

### Phase 6F: Sync Monitoring

### Test 6F.1: Throttled Initial Sync

```bash
# Throttle sync speed BEFORE creating the PVC
oc debug node/$NODE -- chroot /host bash -c '
  echo 1 > /proc/sys/dev/raid/speed_limit_max
  echo 1 > /proc/sys/dev/raid/speed_limit_min
'
```

Create raid5 + PVC. Immediately check:

```bash
oc exec -n $NS $VG_POD -- curl -s http://localhost:8080/metrics | grep lvms_raid_sync
```

**Expected:**
```text
lvms_raid_sync_in_progress{...} 1
lvms_raid_sync_percent{...} <100   (and increasing slowly)
```

Status CR should show `lvHealth[].syncPercent` < 100.

```bash
# Restore sync speed after verification
oc debug node/$NODE -- chroot /host bash -c '
  echo 200000 > /proc/sys/dev/raid/speed_limit_max
  echo 1000 > /proc/sys/dev/raid/speed_limit_min
'
```

### Test 6F.2: RAIDSyncSlow Alert Rule Exists

```bash
oc get prometheusrules -n $NS -o yaml | grep -A10 LVMSRAIDSyncSlow
```

**Expected**: Alert rule present with `for: 30m` and `lvms_raid_sync_in_progress == 1`.

### Test 6F.3: Multiple LVs — minSyncPercent Picks Slowest

1. Throttle sync speed (as in 6F.1)
2. Create raid5 cluster
3. Create PVC-A (100Mi), wait a few seconds for partial sync
4. Create PVC-B (100Mi) — this one starts syncing later, so lower percent

```bash
oc get lvmvolumegroupnodestatuses -n $NS -o jsonpath='{.items[*].status}' | jq '.deviceClassStatuses[] | select(.name=="raid5-dc") | .raidStatus'
```

**Expected:**
- `lvHealth` contains 2 entries with different `syncPercent` values
- `minSyncPercent` equals the lower of the two
- `lvms_raid_sync_percent` metric equals `minSyncPercent`

5. Restore sync speed, wait for completion
6. **Verify**: Both LVs reach 100%, `minSyncPercent: 100`, `lvms_raid_sync_in_progress: 0`

---

## Phase 7: Guardrail Defense

### Test 7.1: VolumeSnapshot Rejection

**Setup**: raid1 cluster with PVC.

```yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: raid-snapshot-test
spec:
  volumeSnapshotClassName: lvms-raid1-dc
  source:
    persistentVolumeClaimName: raid-test-pvc
```

**Expected**: Rejected — no VolumeSnapshotClass exists for RAID DCs.

### Test 7.2: No VolumeSnapshotClass for RAID

```bash
oc get volumesnapshotclasses -o name
```

**Expected**: No entry for the RAID device class StorageClass. Only thin-pool DCs get VolumeSnapshotClasses.

### Test 7.3: StorageClass is Thick Provisioned

```bash
oc get sc lvms-raid1-dc -o yaml
```

**Expected**: No thin-pool-related parameters. Should look like a standard thick StorageClass.

### Test 7.4: Device Removal Blocked When Below Minimum

1. Create raid1 with exactly 2 devices
2. Attempt to remove one path from the CR (leaving only 1)

**Expected**: Rejected — raid1 needs at least 2 devices.

### Test 7.5: Deletion Blocked by Active PVCs (Finalizer)

1. Create raid1 + PVC (don't delete PVC)
2. `oc delete lvmcluster raid-test -n $NS`
3. **Verify**: LVMCluster gets `DeletionTimestamp` but is not removed

```bash
oc get lvmcluster raid-test -n $NS -o jsonpath='{.metadata.deletionTimestamp}'
```

**Expected**: Timestamp set, but resource still exists.

4. Delete PVC
5. **Verify**: LVMCluster now deletes fully

### Test 7.6: ForceWipe with RAID

```yaml
deviceClasses:
- name: raid-wipe-dc
  raidConfig:
    type: raid1
  deviceSelector:
    paths: [$LOOP0, $LOOP1]
    forceWipeDevicesAndDestroyAllData: true
```

1. Pre-write some data to the loop devices: `dd if=/dev/urandom of=$LOOP0 bs=1M count=1`
2. Apply CR
3. **Verify**: Devices wiped, VG created successfully, RAID functional

### Test 7.7: Dynamic Discovery Ignored for RAID

If deviceDiscoveryPolicy were to matter, extra unassigned loop devices might get auto-added to the RAID VG.

1. Create raid1 with 2 specific paths
2. Leave 8 other loop devices unassigned
3. **Verify**: VG has exactly 2 PVs — no auto-discovery happened

```bash
oc debug node/$NODE -- chroot /host pvs --noheadings -o pv_name,vg_name | grep "<raid-vg-name>"
```

**Expected**: Only $LOOP0 and $LOOP1 listed.

---

## Phase 8: Cleanup

### 8.1 Delete All Test Resources

```bash
# Delete pods
oc delete pods -l app=raid-test -n $NS --force --grace-period=0 2>/dev/null

# Delete PVCs created by tests
oc delete pvc raid-test-pvc raid-degraded-pvc -n $NS --ignore-not-found 2>/dev/null

# Delete LVMClusters created by tests
oc delete lvmcluster raid-test raid-immutable-test -n $NS --ignore-not-found

# Wait for finalizers
oc wait --for=delete lvmcluster raid-test raid-immutable-test -n $NS --timeout=300s 2>/dev/null
```

### 8.2 Verify No Orphaned Resources

```bash
oc get lvmvolumegroups -n $NS
oc get lvmvolumegroupnodestatuses -n $NS
oc get pv | grep lvms
oc get sc | grep lvms
oc get volumesnapshotclasses | grep lvms
```

**Expected**: All RAID-related resources gone.

### 8.3 Clean Up Loop Devices on Node

```bash
oc debug node/$NODE -- chroot /host bash -c '
  # Detach all test loop devices
  for f in /var/tmp/raid-test-loop*.img; do
    LOOP=$(losetup -j "$f" --noheadings -O NAME 2>/dev/null | tr -d " ")
    if [ -n "$LOOP" ]; then
      losetup -d "$LOOP" && echo "Detached: $LOOP"
    fi
  done
  # Remove backing files
  rm -f /var/tmp/raid-test-loop*.img
  echo "Cleanup complete"
'
```

### 8.4 Verify Node Is Clean

```bash
oc debug node/$NODE -- chroot /host bash -c '
  echo "=== Loop devices ==="
  losetup -l | grep raid-test || echo "None"
  echo "=== VGs ==="
  vgs --noheadings 2>/dev/null | grep -v "No volume groups" || echo "None"
  echo "=== Backing files ==="
  ls /var/tmp/raid-test-* 2>/dev/null || echo "None"
'
```

### 8.5 Restore Sync Speed (If Throttled)

```bash
oc debug node/$NODE -- chroot /host bash -c '
  echo 200000 > /proc/sys/dev/raid/speed_limit_max
  echo 1000 > /proc/sys/dev/raid/speed_limit_min
  echo "Sync speed restored"
'
```

---

## Test Summary

| Phase | Tests | Focus |
|-------|-------|-------|
| 1. Setup | 5 | Loop devices, prerequisites |
| 2. Happy Path | 12 | All RAID levels, all config fields, data integrity |
| 3. Validation | 18 | Every webhook/CEL rejection path |
| 4. Immutability | 6 | Update rejection + allowed path addition |
| 5. Day-2 Ops | 7 | Expansion, multiple PVCs, optional paths, runtime validation |
| 6. Failure & Recovery | 34 | Degraded/Failed states, metrics at every transition, sync monitoring, multi-LV minSyncPercent |
| 7. Guardrails | 7 | Snapshot rejection, thick SC, finalizers, force-wipe, discovery isolation |
| 8. Cleanup | 5 | Ordered teardown, orphan check |
| **Total** | **94** | |

## Execution Notes

- Tests in each phase run sequentially (each test cleans up before the next)
- Tests across phases are independent — you can re-run any phase
- Failure in Phase 6 tests requires manual loop device recreation before continuing
- All CRs use `nodeSelector` to pin to the chosen node
- Metric checks require vg-manager pod to have the metrics endpoint (port 8080)
- Alert checks require access to Prometheus/Thanos in openshift-monitoring namespace
