---
description: Create an LVM Operator override snapshot in Konflux based off a specified bundle image
argument-hint: [BUNDLE_IMAGE_REF]
allowed-tools:
  - Read(hack/generate_override_snapshot_yaml.sh)
  - Read(snapshots/**)
  - Bash(hack/generate_override_snapshot_yaml.sh:*)
  - Bash(oc login:*)
  - Bash(oc status:*)
  - Bash(oc project:*)
  - Bash(oc whoami:*)
  - Bash(oc apply:*)
  - Bash(oc get snapshot:*)
  - AskUserQuestion
---

# Create Snapshot

Create a konflux override snapshot for the LVM Operator.

## Prerequisites
1. The user must have the following CLIs:
	- `oc`
	- `hack/generate_override_snapshot_yaml.sh`

2. The user must be logged into the konflux cluster
	- `oc status` should show the user logged into the server `https://api.stone-prd-rh01.pg1f.p1.openshiftapps.com:6443`
	- If the user is not logged in, the user will need to run `oc login --web https://api.stone-prd-rh01.pg1f.p1.openshiftapps.com:6443/` to log into the konflux server.
		Prompt the user to do the login and wait for them to confirm they have logged in before continuing.
	
3. The user must have access to the `logical-volume-manag-tenant` project/namespace on the cluster: `oc project logical-volume-manag-tenant`

## Workflow

1. Run the `hack/generate_override_snapshot_yaml.sh` script passing [BUNDLE_IMAGE_REF] as a positional arg. Save the output.
2. Write the output to a file in the snapshots folder matching the name field from the yaml output
3. Ask the user "Do you want to apply the snapshot yaml now, or do you want to apply it later?"
4. If the user wants to apply the yaml now:
	- Immediately before applying, validate the cluster context with `oc whoami --show-server` and `oc project -q`, confirming the server is `https://api.stone-prd-rh01.pg1f.p1.openshiftapps.com:6443` and the project is `logical-volume-manag-tenant`. If either does not match, stop and ask the user to fix their context before continuing.
	- run `oc apply -n logical-volume-manag-tenant -f` with the path to the generated yaml file.
	- monitor the snapshot apply to make sure it is applied successfully by starting a background poller (Bash tool, `run_in_background: true`) to track the integration test status without blocking the conversation. Every poll command must include the validated `-n logical-volume-manag-tenant` namespace and target the validated server context so the poller cannot drift to a different cluster or project if the kubeconfig changes later. Poll `oc get snapshot <name> -n logical-volume-manag-tenant -o jsonpath='{range .status.conditions[*]}{.type}{"="}{.status}{" ("}{.reason}{") "}{end}'` every ~20s, logging each check, and stop once the `AppStudioIntegrationStatus` condition's reason reaches a terminal state (`Finished`, `Failed`, or `Error`). Cap the loop at roughly 30 minutes. On exit, dump the full `oc get snapshot ... -o yaml` for the final state. Report the outcome to the user once the poller finishes.
5. If the user does not want to apply the yaml now, provide them a path to the generated snapshot
