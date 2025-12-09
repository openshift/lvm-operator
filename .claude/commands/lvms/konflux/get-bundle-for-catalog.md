# Get Bundle Version from Catalog

Extract and display the bundle image reference and version information from an OLM catalog image in Konflux.

## Purpose
Query catalog images (directly or via snapshots) to extract bundle version information for LVMS catalogs across different OpenShift versions.

## Prerequisites
- Authenticated with Konflux (run `/lvms:konflux:login` first)
- Access to the `logical-volume-manag-tenant` namespace
- OpenShift CLI (`oc`) installed
- `oc-ka` plugin installed (for KubeArchive access)
- `jq` installed

## How to run this command
This is a Claude Code slash command. To use it, type one of the following in your Claude Code chat:

```bash
# Get bundle from specific catalog image
/lvms:konflux:get-bundle-for-catalog quay.io/redhat-user-workloads/org/catalog@sha256:abc123

# Get bundle from snapshot name
/lvms:konflux:get-bundle-for-catalog --snapshot lvm-operator-catalog-4-18-t2sc5

# Get latest staging bundle for a version
/lvms:konflux:get-bundle-for-catalog --version 4.18 --env staging

# Get latest production bundle for a version
/lvms:konflux:get-bundle-for-catalog --version 4.18 --env production

# Get bundle in JSON format
/lvms:konflux:get-bundle-for-catalog --version 4.18 --format json
```

## Input Options
You must provide ONE of:
- Direct catalog image reference (positional argument)
- `--snapshot NAME` - Catalog snapshot name
- `--version VER` - Get latest catalog for this version (e.g., 4.18)

Optional flags:
- `--env ENV` - Filter by environment: `staging` or `production` (use with --version)
- `--format FORMAT` - Output format: `text`, `json`, or `yaml` (default: text)
- `--namespace NS` - Override namespace (default: logical-volume-manag-tenant)
- `--debug` - Enable debug output
- `--no-cleanup` - Don't cleanup temporary files

## Environment Filtering
- **staging**: Snapshots released to staging environment (AutoReleased without production release)
- **production**: Snapshots released to production environment (actual Release objects exist)
- **no filter**: Latest snapshot with AutoReleased status

## Output
Displays bundle information including:
- Package name
- Bundle version (e.g., 4.18.4)
- Bundle name
- Bundle image reference

## Technical Details
The script:
1. Uses `oc ka` to query both archived and active Release/Snapshot objects
2. For version queries, looks up Release objects by release plan pattern to find snapshots
3. Extracts catalog image from snapshot spec
4. Downloads catalog `/configs/` directory using `oc image extract`
5. Parses OLM bundle metadata and selects latest semantic version

## Implementation
Execute the bash script located at `hack/get-bundle-for-catalog.sh` with the user's arguments.
