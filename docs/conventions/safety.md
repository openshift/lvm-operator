# Safety and Security Conventions

For design principles, see [core-beliefs.md](../core-beliefs.md). These conventions were extracted from PR review threads and may not all reflect current practice — verify against current code when in doubt.

- Separate service accounts per workload with minimal RBAC. VG Manager RBAC files are prefixed with `vg_manager_`; operator RBAC files use generic names. (#14, #21)
- SCC permissions should have comments explaining why they are required (convention from PR #35, not currently followed in code). HostPID is needed because lvmd uses nsenter. Topolvm node requires OpenShift `privileged` SCC — custom SCCs derived from PSPs are insufficient. (#35, #14)
- SCCs were historically preferred as static definitions; operator-managed SCCs were an OCP/OLM workaround. This may have changed with newer OLM versions. (#101)
- Socket/config file paths must use shared constants from the controllers package — a mismatch between vgmanager and topolvm-node caused a real bug. (#27)
- Always handle errors from LVM host commands. Never silently swallow with `if err == nil` guards and no else-branch. All LVM commands accessing lvmdevices must run AsHost. (#79, #459)
- Always nil-check annotation/label maps before key access on Kubernetes objects. (#683)
- Structs with pointer fields cannot be deep-copied by simple dereference — use generated DeepCopy methods. (#708)
- Commands writing to both stdout and stderr must use `CombinedOutput`. `wipefs`/`dmsetup` can issue stderr after stdout closes, causing race conditions. (#687)
- VGManager must not report healthy until CSI driver registration is confirmed in kubelet. (#642)
- Changing `spec.selector` on Deployments/DaemonSets is an immutable field change that breaks upgrades. Add labels to pod templates only. (#148)
- Personal registry references must never be committed to kustomization.yaml or manager.env. (#293)
- Verify dependency upgrades with local deployment — certain logr + controller-runtime version combinations break. (#136)
- DevicePath type wraps string — symlink resolution must happen at-most-once per reconcile. (#690)
- No bind mount filter exists in the current filter chain. Bind mounts are documented as unsupported in known-limitations.md. (#533)
- VG tagging uses `--addtag` as part of the `vgcreate` command (atomic with VG creation). `AddTagToVG` for existing VGs returns a hard error on failure. (#447)
- Feature APIs must wait for release branch cut before landing on main. (#2339)
- Stale string literals survive refactoring when forking upstream projects — audit during fork operations. (#8)
- Namespace changes require upgrade testing validation before merge. Use `/hold` to gate. (#955)
- Resource leak on command output failure: if `io.ReadAll` fails, the stderr pipe remains open. Pipe-based execution requires careful close ordering. (#778)
