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
# Get ALL versions for an environment (simplest usage)
/lvms:konflux:get-bundle-for-catalog staging
/lvms:konflux:get-bundle-for-catalog production

# Get ALL versions with JSON output
/lvms:konflux:get-bundle-for-catalog --env staging --format json

# Get bundle from specific catalog image
/lvms:konflux:get-bundle-for-catalog quay.io/redhat-user-workloads/org/catalog@sha256:abc123

# Get bundle from snapshot name
/lvms:konflux:get-bundle-for-catalog --snapshot lvm-operator-catalog-4-18-t2sc5

# Get latest staging bundle for a specific version
/lvms:konflux:get-bundle-for-catalog --version 4.18 --env staging

# Get latest production bundle for a specific version
/lvms:konflux:get-bundle-for-catalog --version 4.18 --env production
```

## Input Options
You must provide ONE of:
- Environment name: `staging` or `production` (positional argument) - queries ALL versions
- Direct catalog image reference (positional argument)
- `--snapshot NAME` - Catalog snapshot name
- `--version VER` - Get latest catalog for this version (e.g., 4.18)
- `--env ENV` - Filter by environment: `staging` or `production`

Optional flags:
- `--format FORMAT` - Output format: `text`, `json`, or `yaml` (default: text)
- `--namespace NS` - Override namespace (default: logical-volume-manag-tenant)
- `--debug` - Enable debug output
- `--no-cleanup` - Don't cleanup temporary files

## Environment Filtering
- **staging**: Snapshots released to staging environment (AutoReleased without production release)
- **production**: Snapshots released to production environment (actual Release objects exist)
- **no filter**: Latest snapshot with AutoReleased status

## Output
Displays catalog and bundle information including:
- Catalog snapshot name
- Catalog image reference
- Package name
- Bundle version (e.g., 4.18.4)
- Bundle name
- **Bundle snapshot name** (e.g., lvm-operator-4-20-24xvq)
- Bundle image reference

When querying all versions for an environment, displays information for each version (4.17, 4.18, 4.19, 4.20, 4.21).

## Technical Details
The script:
1. Uses `oc ka` to query both archived and active Release/Snapshot objects
2. For version queries, looks up Release objects by release plan pattern to find snapshots
3. Extracts catalog image from snapshot spec
4. Downloads catalog `/configs/` directory using `oc image extract`
5. Parses OLM bundle metadata and selects latest semantic version
6. **Finds bundle snapshot by checking Release objects** for authoritative mapping
7. Falls back to timestamp-based matching if no Release found

## Implementation
Execute the bash script located at `hack/get-bundle-for-catalog.sh` with the user's arguments.
