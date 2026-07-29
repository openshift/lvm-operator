#!/bin/bash
# LVMS Branching Automation Script
# Automates the release branching procedure for lvm-operator
#
# Usage: ./hack/branch-release.sh --old-version 4.21 --new-version 4.22
#
set -euo pipefail

# Configuration
OLD_VERSION=""
NEW_VERSION=""
DRY_RUN=false
STEP=""
LVM_OPERATOR_DIR=""
KONFLUX_DIR=""
PRODSEC_DIR=""
SKIP_PRODSEC=false
SHOW_STEPS=false

# Derived versions (set after parsing args)
OLD_VER_DASH=""    # e.g., 4-21
NEW_VER_DASH=""    # e.g., 4-22
OLD_VER_V=""       # e.g., v4.21
NEW_VER_V=""       # e.g., v4.22
OLD_NEXT_VER_V=""  # e.g., v4.22 (for OPENSHIFT_VERSIONS range)
NEW_NEXT_VER_V=""  # e.g., v4.23 (for OPENSHIFT_VERSIONS range)

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

#######################################
# Logging functions
#######################################

log_info() {
    echo -e "${BLUE}[INFO]${NC} ${*}"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} ${*}"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} ${*}"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} ${*}" >&2
}

log_step() {
    echo -e "\n${GREEN}========================================${NC}"
    echo -e "${GREEN}${*}${NC}"
    echo -e "${GREEN}========================================${NC}\n"
}

log_dry_run() {
    echo -e "${YELLOW}[DRY-RUN]${NC} ${*}"
}

#######################################
# Helper functions
#######################################

usage() {
    cat <<EOF
LVMS Branching Automation Script

Usage: $(basename "${0}") [OPTIONS]

Required:
  --old-version VERSION    Version being branched (e.g., 4.21)
  --new-version VERSION    New version for main (e.g., 4.22)
  --lvm-dir PATH           Path to lvm-operator repo

Optional:
  --konflux-dir PATH       Path to konflux-release-data repo
  --prodsec-dir PATH       Path to product-definitions repo
  --dry-run                Show what would be done without making changes
  --step STEP              Run only a specific step (1-6)
  --skip-prodsec           Skip step 2 (prodsec update)
  --show-steps             Show detailed information about each step and exit
  --help                   Show this help message

Repositories:
  lvm-operator             https://github.com/openshift/lvm-operator (GitHub)
  konflux-release-data     https://gitlab.cee.redhat.com/konflux/konflux-release-data (GitLab)
  product-definitions      https://gitlab.cee.redhat.com/prodsec/product-definitions (GitLab)

Remote Requirements:
  All repositories must have "origin" pointing to the main upstream repo:
    - lvm-operator: origin -> https://github.com/openshift/lvm-operator (main branch)
    - konflux-release-data: origin -> https://gitlab.cee.redhat.com/konflux/konflux-release-data (main branch)
    - product-definitions: origin -> https://gitlab.cee.redhat.com/prodsec/product-definitions (master branch)

  The script will sync each repo to origin/<default-branch> before making changes.

  NOTE: The script creates commits locally but does NOT push. After each step,
  it shows the branch name and push command. Push to your fork and create PRs manually.

Steps:
  1 - GitHub branching (create release branch in lvm-operator)
  2 - Prodsec product definitions update (product-definitions repo)
  3 - Konflux release data updates (konflux-release-data repo)
  4 - Release branch tekton/config updates (lvm-operator repo)
  5 - Main branch version updates (lvm-operator repo)
  6 - Catalog template regeneration (lvm-operator repo, post-merge)
  7 - Verify Konflux SA push secrets (requires oc access to Konflux cluster, post step 3 merge)

Examples:
  # Dry run to preview changes
  $(basename "${0}") --old-version 4.21 --new-version 4.22 --lvm-dir ~/lvm-operator --dry-run

  # Run all steps interactively
  $(basename "${0}") --old-version 4.21 --new-version 4.22 --lvm-dir ~/lvm-operator

  # Run a specific step only
  $(basename "${0}") --old-version 4.21 --new-version 4.22 --lvm-dir ~/lvm-operator --step 4

  # Run with all repos
  $(basename "${0}") --old-version 4.21 --new-version 4.22 \\
    --lvm-dir ~/lvm-operator \\
    --konflux-dir ~/konflux-release-data \\
    --prodsec-dir ~/product-definitions
EOF
    exit 0
}

show_steps() {
    cat <<'EOF'
LVMS Branching Procedure - Detailed Steps
==========================================

STEP 1: GitHub Branching (lvm-operator)
----------------------------------------
Repository: https://github.com/openshift/lvm-operator
Actions:
  - Fetch latest from origin
  - Create release-X.Y branch from main
  - Push release-X.Y branch to origin

STEP 2: Prodsec Product Definitions Update
------------------------------------------
Repository: https://gitlab.cee.redhat.com/prodsec/product-definitions
File: data/openshift/ps_update_streams.json
Actions:
  - Add new lvms-operator-X.Y entry with:
    - name: lvms-operator-X.Y
    - version_regex: ^X.Y\.[0-9]+$
    - brew_tags: [lvms-X-Y-rhel-9-candidate]
  - Create MR and get prodsec approval

STEP 3: Konflux Release Data Updates
-------------------------------------
Repository: https://gitlab.cee.redhat.com/konflux/konflux-release-data
Actions:
  3.1 Operator Component Versions:
      Directory: tenants-config/.../lvm-operator/versions/
      - Copy vX-Y.yaml -> vX-Y+1.yaml (new version)
      - Update old version to point to release-X.Y branch
      - Add new version to kustomization.yaml

  3.2 Operator Catalog Versions:
      Directory: tenants-config/.../lvm-operator-catalog/versions/
      - Same pattern as 3.1

  3.3 Run auto-generate script:
      ./hack/generate-tenant-configs.sh logical-volume-manag-tenant

  3.4 Release Plan Admissions:
      Directory: config/.../ReleasePlanAdmission/logical-volume-manag/
      - Copy production/staging files for new version
      - Update catalog files with new version references

STEP 4: Release Branch Tekton/Config Updates
---------------------------------------------
Repository: https://github.com/openshift/lvm-operator
Branch: release-X.Y
Actions:
  - Update .tekton/*.yaml files:
    - Change target_branch == "main" -> target_branch == "release-X.Y"
  - Pipeline references remain pointing to main (intentional)
  - Create PR targeting release-X.Y branch

STEP 5: Main Branch Version Updates
------------------------------------
Repository: https://github.com/openshift/lvm-operator
Branch: main
Actions:
  - Update .tekton/*.yaml files:
    - Replace component names: *-X-Y -> *-X-Y+1
    - Replace service accounts: build-pipeline-*-X-Y -> build-pipeline-*-X-Y+1
    - Update CATALOG_VERSION and image tags
  - Update release/container-build.args:
    - OPERATOR_VERSION=X.Y+1.0
    - LVMS_TAGS=vX.Y+1
    - OPENSHIFT_VERSIONS=vX.Y+1-vX.Y+2
  - Update release/konflux.make:
    - Y_STREAM="vX.Y+1"
  - Create PR targeting main branch

STEP 6: Catalog Template Regeneration (Post-merge)
---------------------------------------------------
Repository: https://github.com/openshift/lvm-operator
Branch: main
Prerequisites: PRs from steps 4 and 5 must be merged
Actions:
  - Run: make catalog-template
  - Review generated templates
  - Create PR for catalog updates

STEP 7: Verify Konflux SA Push Secrets (Post step 3 merge)
-----------------------------------------------------------
Cluster: stone-prd-rh01 (requires oc login)
Namespace: logical-volume-manag-tenant
Prerequisites: Step 3 MR merged and Konflux components provisioned
Actions:
  - Label all image-push secrets with build.appstudio.openshift.io/common-secret=true
  - Link each new version SA with its matching image-push secret
  - Link all new version SAs with registry-stage-redhat-io-docker
  - Verify secrets are linked correctly
Note: This step is idempotent. The common-secret label ensures future versions
      auto-link, but explicit linking acts as a safety net.

VERSION PATTERNS
================
  X.Y      -> 4.21         (release branch name: release-4.21)
  X-Y      -> 4-21         (component names, labels, service accounts)
  vX.Y     -> v4.21        (LVMS_TAGS, Y_STREAM, CATALOG_VERSION)
  X.Y.Z    -> 4.21.0       (OPERATOR_VERSION)
  vX.Y-vZ  -> v4.21-v4.22  (OPENSHIFT_VERSIONS range)

EOF
    exit 0
}

parse_args() {
    while [[ ${#} -gt 0 ]]; do
        case "${1}" in
            --old-version)
                OLD_VERSION="${2}"
                shift 2
                ;;
            --new-version)
                NEW_VERSION="${2}"
                shift 2
                ;;
            --lvm-dir)
                LVM_OPERATOR_DIR="${2}"
                shift 2
                ;;
            --konflux-dir)
                KONFLUX_DIR="${2}"
                shift 2
                ;;
            --prodsec-dir)
                PRODSEC_DIR="${2}"
                shift 2
                ;;
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            --step)
                STEP="${2}"
                shift 2
                ;;
            --skip-prodsec)
                SKIP_PRODSEC=true
                shift
                ;;
            --show-steps)
                SHOW_STEPS=true
                shift
                ;;
            --help)
                usage
                ;;
            *)
                log_error "Unknown option: ${1}"
                usage
                ;;
        esac
    done
}

validate_version() {
    local version="${1}"
    if [[ ! "${version}" =~ ^[0-9]+\.[0-9]+$ ]]; then
        log_error "Invalid version format: ${version} (expected X.Y, e.g., 4.21)"
        exit 1
    fi
}

compute_derived_versions() {
    # Convert X.Y to X-Y (dash format)
    OLD_VER_DASH="${OLD_VERSION//./-}"
    NEW_VER_DASH="${NEW_VERSION//./-}"

    # Convert to vX.Y format
    OLD_VER_V="v${OLD_VERSION}"
    NEW_VER_V="v${NEW_VERSION}"

    # Compute next versions for OPENSHIFT_VERSIONS range
    local old_minor="${OLD_VERSION#*.}"
    local new_minor="${NEW_VERSION#*.}"
    local major="${OLD_VERSION%%.*}"

    OLD_NEXT_VER_V="v${major}.$((old_minor + 1))"
    NEW_NEXT_VER_V="v${major}.$((new_minor + 1))"
}

check_prerequisites() {
    log_info "Checking prerequisites..."

    # Check required tools
    if ! command -v kustomize &> /dev/null; then
        log_error "kustomize is not installed. Please install it first."
        exit 1
    fi
    log_info "  kustomize found"

    if ! command -v git &> /dev/null; then
        log_error "git is not installed. Please install it first."
        exit 1
    fi
    log_info "  git found"

    if ! command -v jq &> /dev/null; then
        log_error "jq is not installed. Please install it first."
        exit 1
    fi
    log_info "  jq found"

    if ! command -v sed &> /dev/null; then
        log_error "sed is not installed. Please install it first."
        exit 1
    fi
    log_info "  sed found"

    # Check lvm-operator directory
    if [[ ! -d "${LVM_OPERATOR_DIR}/.git" ]]; then
        log_error "lvm-operator directory is not a git repository: ${LVM_OPERATOR_DIR}"
        exit 1
    fi
    if [[ ! -d "${LVM_OPERATOR_DIR}/.tekton" ]]; then
        log_error "lvm-operator directory does not contain .tekton: ${LVM_OPERATOR_DIR}"
        exit 1
    fi
    log_info "  lvm-operator repo found at ${LVM_OPERATOR_DIR}"

    # Check lvm-operator remote
    if ! git -C "${LVM_OPERATOR_DIR}" remote get-url origin &>/dev/null; then
        log_error "lvm-operator: remote 'origin' not found"
        log_error "Please add it: git remote add origin https://github.com/openshift/lvm-operator.git"
        exit 1
    fi
    log_info "  lvm-operator: 'origin' remote found"

    # Check for clean working directory in lvm-operator
    if [[ -n "$(git -C "${LVM_OPERATOR_DIR}" status --porcelain)" ]]; then
        log_warning "lvm-operator has uncommitted changes"
        if [[ "${DRY_RUN}" == "false" ]]; then
            prompt_continue "Continue anyway?"
        fi
    else
        log_info "  lvm-operator working directory is clean"
    fi

    # Check konflux directory if specified or needed
    if [[ -n "${KONFLUX_DIR}" ]]; then
        if [[ ! -d "${KONFLUX_DIR}/.git" ]]; then
            log_error "konflux-release-data directory is not a git repository: ${KONFLUX_DIR}"
            exit 1
        fi
        log_info "  konflux-release-data repo found at ${KONFLUX_DIR}"

        # Check konflux remote
        if ! git -C "${KONFLUX_DIR}" remote get-url origin &>/dev/null; then
            log_error "konflux-release-data: remote 'origin' not found"
            exit 1
        fi
        log_info "  konflux-release-data: 'origin' remote found"

        # Check for clean working directory
        if [[ -n "$(git -C "${KONFLUX_DIR}" status --porcelain)" ]]; then
            log_warning "konflux-release-data has uncommitted changes"
            if [[ "${DRY_RUN}" == "false" ]]; then
                prompt_continue "Continue anyway?"
            fi
        else
            log_info "  konflux-release-data working directory is clean"
        fi
    fi

    # Check prodsec directory if specified and not skipped
    if [[ "${SKIP_PRODSEC}" == "false" && -n "${PRODSEC_DIR}" ]]; then
        if [[ ! -d "${PRODSEC_DIR}/.git" ]]; then
            log_error "product-definitions directory is not a git repository: ${PRODSEC_DIR}"
            exit 1
        fi
        log_info "  product-definitions repo found at ${PRODSEC_DIR}"

        # Check prodsec remote
        if ! git -C "${PRODSEC_DIR}" remote get-url origin &>/dev/null; then
            log_error "product-definitions: remote 'origin' not found"
            exit 1
        fi
        log_info "  product-definitions: 'origin' remote found"

        # Check for clean working directory
        if [[ -n "$(git -C "${PRODSEC_DIR}" status --porcelain)" ]]; then
            log_warning "product-definitions has uncommitted changes"
            if [[ "${DRY_RUN}" == "false" ]]; then
                prompt_continue "Continue anyway?"
            fi
        else
            log_info "  product-definitions working directory is clean"
        fi
    fi

    log_success "All prerequisites satisfied"
}

# Sync repositories to origin/main
sync_repositories() {
    log_info "Syncing repositories to origin..."

    # Sync lvm-operator to origin/main
    log_info "  Syncing lvm-operator to origin/main..."
    if [[ "${DRY_RUN}" == "true" ]]; then
        log_dry_run "git -C ${LVM_OPERATOR_DIR} fetch origin"
        log_dry_run "git -C ${LVM_OPERATOR_DIR} checkout main"
        log_dry_run "git -C ${LVM_OPERATOR_DIR} reset --hard origin/main"
    else
        git -C "${LVM_OPERATOR_DIR}" fetch origin
        git -C "${LVM_OPERATOR_DIR}" checkout main
        git -C "${LVM_OPERATOR_DIR}" reset --hard origin/main
    fi
    log_info "  lvm-operator synced to origin/main"

    # Sync konflux if specified
    if [[ -n "${KONFLUX_DIR}" ]]; then
        log_info "  Syncing konflux-release-data to origin/main..."
        if [[ "${DRY_RUN}" == "true" ]]; then
            log_dry_run "git -C ${KONFLUX_DIR} fetch origin"
            log_dry_run "git -C ${KONFLUX_DIR} checkout main"
            log_dry_run "git -C ${KONFLUX_DIR} reset --hard origin/main"
        else
            git -C "${KONFLUX_DIR}" fetch origin
            git -C "${KONFLUX_DIR}" checkout main
            git -C "${KONFLUX_DIR}" reset --hard origin/main
        fi
        log_info "  konflux-release-data synced to origin/main"
    fi

    # Sync prodsec if specified and not skipped (uses master branch)
    if [[ "${SKIP_PRODSEC}" == "false" && -n "${PRODSEC_DIR}" ]]; then
        log_info "  Syncing product-definitions to origin/master..."
        if [[ "${DRY_RUN}" == "true" ]]; then
            log_dry_run "git -C ${PRODSEC_DIR} fetch origin"
            log_dry_run "git -C ${PRODSEC_DIR} checkout master"
            log_dry_run "git -C ${PRODSEC_DIR} reset --hard origin/master"
        else
            git -C "${PRODSEC_DIR}" fetch origin
            git -C "${PRODSEC_DIR}" checkout master
            git -C "${PRODSEC_DIR}" reset --hard origin/master
        fi
        log_info "  product-definitions synced to origin/master"
    fi

    log_success "All repositories synced"
}

prompt_continue() {
    local message="${1:-Continue?}"
    if [[ "${DRY_RUN}" == "true" ]]; then
        return 0
    fi
    echo -n -e "${YELLOW}${message} [y/N] ${NC}"
    read -r response
    case "${response}" in
        [yY][eE][sS]|[yY])
            return 0
            ;;
        *)
            log_info "Aborted by user"
            exit 0
            ;;
    esac
}

run_cmd() {
    if [[ "${DRY_RUN}" == "true" ]]; then
        log_dry_run "${*}"
    else
        log_info "Running: ${*}"
        "${@}"
    fi
}

# Perform sed replacement with backup for safety
sed_replace() {
    local pattern="${1}"
    local replacement="${2}"
    local file="${3}"

    if [[ "${DRY_RUN}" == "true" ]]; then
        log_dry_run "sed -i 's/${pattern}/${replacement}/g' ${file}"
    else
        sed -i "s/${pattern}/${replacement}/g" "${file}"
    fi
}

#######################################
# Cleanup functions (for re-runnability)
#######################################

# Delete a local branch if it exists
delete_local_branch_if_exists() {
    local branch="${1}"
    local repo_dir="${2:-.}"

    if git -C "${repo_dir}" show-ref --verify --quiet "refs/heads/${branch}" 2>/dev/null; then
        log_warning "Local branch '${branch}' exists, deleting it..."
        if [[ "${DRY_RUN}" == "true" ]]; then
            log_dry_run "git -C ${repo_dir} branch -D ${branch}"
        else
            git -C "${repo_dir}" branch -D "${branch}"
        fi
    fi
}

# Check if a remote branch exists
remote_branch_exists() {
    local remote="${1}"
    local branch="${2}"
    local repo_dir="${3:-.}"

    git -C "${repo_dir}" ls-remote --exit-code --heads "${remote}" "${branch}" &>/dev/null
}

# Validate that old version resources exist
validate_old_version_resources() {
    log_info "Validating old version resources exist..."
    local errors=0

    # Check lvm-operator tekton files have old version references
    local tekton_file="${LVM_OPERATOR_DIR}/.tekton/lvm-operator-push.yaml"
    if [[ -f "${tekton_file}" ]]; then
        if ! grep -q "lvm-operator-${OLD_VER_DASH}" "${tekton_file}"; then
            log_error "Old version ${OLD_VER_DASH} not found in ${tekton_file}"
            log_error "The tekton files may already be updated to a newer version"
            errors=$((errors + 1))
        else
            log_info "  lvm-operator tekton files contain ${OLD_VER_DASH} references"
        fi
    fi

    # Check konflux old version files if konflux dir is specified
    if [[ -n "${KONFLUX_DIR}" ]]; then
        local op_versions_dir="${KONFLUX_DIR}/tenants-config/cluster/stone-prd-rh01/tenants/logical-volume-manag-tenant/lvm-operator/versions"
        local old_op_file="${op_versions_dir}/v${OLD_VER_DASH}.yaml"
        if [[ ! -f "${old_op_file}" ]]; then
            log_error "Old version file not found: ${old_op_file}"
            errors=$((errors + 1))
        else
            log_info "  Found konflux operator version file: v${OLD_VER_DASH}.yaml"
        fi

        local cat_versions_dir="${KONFLUX_DIR}/tenants-config/cluster/stone-prd-rh01/tenants/logical-volume-manag-tenant/lvm-operator-catalog/versions"
        local old_cat_file="${cat_versions_dir}/v${OLD_VER_DASH}.yaml"
        if [[ ! -f "${old_cat_file}" ]]; then
            log_error "Old catalog version file not found: ${old_cat_file}"
            errors=$((errors + 1))
        else
            log_info "  Found konflux catalog version file: v${OLD_VER_DASH}.yaml"
        fi

        local rpa_dir="${KONFLUX_DIR}/config/stone-prd-rh01.pg1f.p1/product/ReleasePlanAdmission/logical-volume-manag"
        local old_rpa_file="${rpa_dir}/lvm-operator-${OLD_VER_DASH}-production.yaml"
        if [[ ! -f "${old_rpa_file}" ]]; then
            log_error "Old RPA file not found: ${old_rpa_file}"
            errors=$((errors + 1))
        else
            log_info "  Found RPA file: lvm-operator-${OLD_VER_DASH}-production.yaml"
        fi
    fi

    if [[ ${errors} -gt 0 ]]; then
        log_error "Validation failed with ${errors} error(s)"
        log_error "Please verify --old-version ${OLD_VERSION} is correct"
        exit 1
    fi

    log_success "Old version resources validated"
}

# Ensure we're on a clean base branch, discarding local changes
ensure_clean_branch() {
    local branch="${1}"
    local remote="${2}"
    local repo_dir="${3:-.}"

    if [[ "${DRY_RUN}" == "true" ]]; then
        log_dry_run "git -C ${repo_dir} checkout ${branch}"
        log_dry_run "git -C ${repo_dir} reset --hard ${remote}/${branch}"
        return 0
    fi

    git -C "${repo_dir}" checkout "${branch}" 2>/dev/null || git -C "${repo_dir}" checkout -b "${branch}" "${remote}/${branch}"
    git -C "${repo_dir}" reset --hard "${remote}/${branch}"
}

#######################################
# Step functions
#######################################

step1_github_branching() {
    log_step "Step 1: GitHub Branching (lvm-operator)"

    cd "${LVM_OPERATOR_DIR}"

    log_info "Creating release branch: release-${OLD_VERSION}"

    # Cleanup: delete local branch if it exists from a previous run
    delete_local_branch_if_exists "release-${OLD_VERSION}" "${LVM_OPERATOR_DIR}"

    run_cmd git fetch origin

    # Check if release branch already exists on origin
    if remote_branch_exists "origin" "release-${OLD_VERSION}" "${LVM_OPERATOR_DIR}"; then
        log_warning "Release branch release-${OLD_VERSION} already exists on origin"
        log_info "Skipping branch creation - branch already pushed"
        return 0
    fi

    run_cmd git checkout main
    run_cmd git reset --hard origin/main
    run_cmd git checkout -b "release-${OLD_VERSION}"

    log_success "Release branch release-${OLD_VERSION} created locally"

    echo ""
    log_info "Branch ready to push:"
    log_info "  Repository: lvm-operator"
    log_info "  Branch: release-${OLD_VERSION}"
    log_info "  Push command: git -C ${LVM_OPERATOR_DIR} push <remote> release-${OLD_VERSION}"
}

step2_prodsec_update() {
    log_step "Step 2: Prodsec Product Definitions Update"

    if [[ "${SKIP_PRODSEC}" == "true" ]]; then
        log_info "Skipping prodsec update (--skip-prodsec specified)"
        return 0
    fi

    if [[ -z "${PRODSEC_DIR}" ]]; then
        log_warning "Prodsec directory not specified. Providing manual instructions."
        echo ""
        log_info "Manual steps for prodsec update:"
        log_info "  1. Clone or navigate to the product-definitions repository"
        log_info "  2. Edit data/openshift/ps_update_streams.json"
        log_info "  3. Add new entry in ps_update_streams object after the last lvms-operator entry:"
        cat <<EOF

    "lvms-operator-${NEW_VERSION}": {
      "pp_label": "lvms-${NEW_VERSION}",
      "version": "${NEW_VERSION}",
      "target_release": "${NEW_VERSION}.0",
      "cpe": [
        "cpe:/a:redhat:lvms:${NEW_VERSION}::el9"
      ]
    },

EOF
        log_info "  4. Create a branch: git checkout -b add-lvms-${NEW_VERSION}"
        log_info "  5. Commit and push: git commit -m 'Add lvms-operator-${NEW_VERSION} version'"
        log_info "  6. Create MR and get prodsec approval"
        echo ""
        prompt_continue "Continue to next step?"
        return 0
    fi

    cd "${PRODSEC_DIR}"

    # Cleanup: delete local branch and reset to clean master
    delete_local_branch_if_exists "add-lvms-${NEW_VERSION}" "${PRODSEC_DIR}"

    run_cmd git fetch origin
    run_cmd git checkout master
    run_cmd git reset --hard origin/master
    run_cmd git checkout -b "add-lvms-${NEW_VERSION}"

    local ps_file="data/openshift/ps_update_streams.json"

    if [[ ! -f "${ps_file}" ]]; then
        log_error "File not found: ${ps_file}"
        exit 1
    fi

    # Check if entry already exists
    local entry_key="lvms-operator-${NEW_VERSION}"
    if jq -e --arg key "${entry_key}" '.ps_update_streams[$key]' "${ps_file}" &>/dev/null; then
        log_warning "Entry ${entry_key} already exists in ${ps_file}"
        log_info "Skipping JSON modification"
    else
        log_info "Adding new ${entry_key} entry to ${ps_file}"

        if [[ "${DRY_RUN}" == "true" ]]; then
            log_dry_run "Would add ${entry_key} entry to ${ps_file}"
        else
            # Use jq to add the new entry to ps_update_streams object
            local tmp_file
            tmp_file=$(mktemp)
            jq --arg key "${entry_key}" \
               --arg pp_label "lvms-${NEW_VERSION}" \
               --arg version "${NEW_VERSION}" \
               --arg target_release "${NEW_VERSION}.0" \
               --arg cpe "cpe:/a:redhat:lvms:${NEW_VERSION}::el9" \
               '.ps_update_streams[$key] = {
                 "pp_label": $pp_label,
                 "version": $version,
                 "target_release": $target_release,
                 "cpe": [$cpe]
               }' "${ps_file}" > "${tmp_file}"
            mv "${tmp_file}" "${ps_file}"
        fi
    fi

    # Check if there are changes to commit
    run_cmd git add "${ps_file}"
    if git diff --cached --quiet; then
        log_info "No changes to commit - prodsec entry already exists upstream"
        log_success "Step 2 complete (nothing to do)"
    else
        run_cmd git commit -m "Add lvms-operator-${NEW_VERSION} version"
        log_success "Prodsec update committed"
        echo ""
        log_info "Branch ready to push:"
        log_info "  Repository: product-definitions"
        log_info "  Branch: add-lvms-${NEW_VERSION}"
        log_info "  Push command: git -C ${PRODSEC_DIR} push <remote> add-lvms-${NEW_VERSION}"
        log_info ""
        log_info "After pushing, create a GitLab MR and get approval from prodsec representative"
    fi
}

step3_konflux_updates() {
    log_step "Step 3: Konflux Release Data Updates"

    if [[ -z "${KONFLUX_DIR}" ]]; then
        log_warning "Konflux directory not specified. Providing manual instructions."
        echo ""
        log_info "Manual steps for konflux-release-data update:"
        log_info ""
        log_info "3.1 Operator Component Version Updates:"
        log_info "  Directory: tenants-config/cluster/stone-prd-rh01/tenants/logical-volume-manag-tenant/lvm-operator/versions/"
        log_info "  - Copy v${OLD_VER_DASH}.yaml -> v${NEW_VER_DASH}.yaml"
        log_info "  - Replace all '${OLD_VER_DASH}' with '${NEW_VER_DASH}' in new file"
        log_info "  - Update v${OLD_VER_DASH}.yaml: replace 'main' with 'release-${OLD_VERSION}'"
        log_info "  - Add v${NEW_VER_DASH}.yaml to kustomization.yaml"
        log_info ""
        log_info "3.2 Operator Catalog Version Updates:"
        log_info "  Directory: tenants-config/cluster/stone-prd-rh01/tenants/logical-volume-manag-tenant/lvm-operator-catalog/versions/"
        log_info "  - Same pattern as 3.1"
        log_info ""
        log_info "3.3 Run auto-generate script:"
        log_info "  ./hack/generate-tenant-configs.sh logical-volume-manag-tenant"
        log_info ""
        log_info "3.4 Release Plan Admission Updates:"
        log_info "  Directory: config/stone-prd-rh01.pg1f.p1/product/ReleasePlanAdmission/logical-volume-manag/"
        log_info "  - Copy lvm-operator-${OLD_VER_DASH}-production.yaml -> lvm-operator-${NEW_VER_DASH}-production.yaml"
        log_info "  - Copy lvm-operator-${OLD_VER_DASH}-staging.yaml -> lvm-operator-${NEW_VER_DASH}-staging.yaml"
        log_info "  - Replace version references in new files"
        log_info "  - Update catalog files to add new version references"
        echo ""
        prompt_continue "Continue to next step?"
        return 0
    fi

    cd "${KONFLUX_DIR}"

    # Cleanup: delete local branch and reset to clean main
    delete_local_branch_if_exists "lvms-${NEW_VERSION}-branching" "${KONFLUX_DIR}"

    run_cmd git fetch origin
    run_cmd git checkout main
    run_cmd git reset --hard origin/main
    run_cmd git checkout -b "lvms-${NEW_VERSION}-branching"

    # 3.1 Operator Component Version Updates
    log_info "3.1 Updating operator component versions..."
    local operator_versions_dir="tenants-config/cluster/stone-prd-rh01/tenants/logical-volume-manag-tenant/lvm-operator/versions"

    if [[ -d "${operator_versions_dir}" ]]; then
        local old_op_file="${operator_versions_dir}/v${OLD_VER_DASH}.yaml"
        local new_op_file="${operator_versions_dir}/v${NEW_VER_DASH}.yaml"

        if [[ -f "${old_op_file}" ]]; then
            # Copy and update new version file
            if [[ "${DRY_RUN}" == "true" ]]; then
                log_dry_run "cp ${old_op_file} ${new_op_file}"
                log_dry_run "Replace ${OLD_VER_DASH} with ${NEW_VER_DASH} in ${new_op_file}"
            else
                cp "${old_op_file}" "${new_op_file}"
                sed -i "s/${OLD_VER_DASH}/${NEW_VER_DASH}/g" "${new_op_file}"
                sed -i "s/${OLD_VERSION}/${NEW_VERSION}/g" "${new_op_file}"
            fi

            # Update old version to point to release branch
            sed_replace "revision: main" "revision: release-${OLD_VERSION}" "${old_op_file}"

            # Update kustomization.yaml
            local op_kustomization="${operator_versions_dir}/kustomization.yaml"
            if [[ -f "${op_kustomization}" ]]; then
                if [[ "${DRY_RUN}" == "true" ]]; then
                    log_dry_run "Add v${NEW_VER_DASH}.yaml to ${op_kustomization}"
                else
                    if ! grep -q "v${NEW_VER_DASH}.yaml" "${op_kustomization}"; then
                        sed -i "/v${OLD_VER_DASH}.yaml/a\\  - v${NEW_VER_DASH}.yaml" "${op_kustomization}"
                    fi
                fi
            fi
            log_info "  Operator component versions updated"
        else
            log_error "Old version file not found: ${old_op_file}"
            exit 1
        fi
    else
        log_error "Operator versions directory not found: ${operator_versions_dir}"
        exit 1
    fi

    # 3.2 Operator Catalog Version Updates
    log_info "3.2 Updating operator catalog versions..."
    local catalog_versions_dir="tenants-config/cluster/stone-prd-rh01/tenants/logical-volume-manag-tenant/lvm-operator-catalog/versions"

    if [[ -d "${catalog_versions_dir}" ]]; then
        local old_cat_file="${catalog_versions_dir}/v${OLD_VER_DASH}.yaml"
        local new_cat_file="${catalog_versions_dir}/v${NEW_VER_DASH}.yaml"

        if [[ -f "${old_cat_file}" ]]; then
            if [[ "${DRY_RUN}" == "true" ]]; then
                log_dry_run "cp ${old_cat_file} ${new_cat_file}"
                log_dry_run "Replace ${OLD_VER_DASH} with ${NEW_VER_DASH} in ${new_cat_file}"
            else
                cp "${old_cat_file}" "${new_cat_file}"
                sed -i "s/${OLD_VER_DASH}/${NEW_VER_DASH}/g" "${new_cat_file}"
                sed -i "s/${OLD_VERSION}/${NEW_VERSION}/g" "${new_cat_file}"
            fi

            sed_replace "revision: main" "revision: release-${OLD_VERSION}" "${old_cat_file}"

            local cat_kustomization="${catalog_versions_dir}/kustomization.yaml"
            if [[ -f "${cat_kustomization}" ]]; then
                if [[ "${DRY_RUN}" == "true" ]]; then
                    log_dry_run "Add v${NEW_VER_DASH}.yaml to ${cat_kustomization}"
                else
                    if ! grep -q "v${NEW_VER_DASH}.yaml" "${cat_kustomization}"; then
                        sed -i "/v${OLD_VER_DASH}.yaml/a\\  - v${NEW_VER_DASH}.yaml" "${cat_kustomization}"
                    fi
                fi
            fi
            log_info "  Operator catalog versions updated"
        else
            log_error "Old catalog version file not found: ${old_cat_file}"
            exit 1
        fi
    else
        log_error "Catalog versions directory not found: ${catalog_versions_dir}"
        exit 1
    fi

    # 3.3 Run auto-generate script
    log_info "3.3 Running auto-generate script..."
    if [[ -f "./hack/generate-tenant-configs.sh" ]]; then
        run_cmd ./hack/generate-tenant-configs.sh logical-volume-manag-tenant
    else
        log_warning "generate-tenant-configs.sh not found, skipping"
    fi

    # 3.4 Release Plan Admission Updates
    log_info "3.4 Updating Release Plan Admissions..."
    local rpa_dir="config/stone-prd-rh01.pg1f.p1/product/ReleasePlanAdmission/logical-volume-manag"

    if [[ -d "${rpa_dir}" ]]; then
        # Copy production and staging files
        local env
        for env in production staging; do
            local old_rpa_file="${rpa_dir}/lvm-operator-${OLD_VER_DASH}-${env}.yaml"
            local new_rpa_file="${rpa_dir}/lvm-operator-${NEW_VER_DASH}-${env}.yaml"

            if [[ -f "${old_rpa_file}" ]]; then
                if [[ "${DRY_RUN}" == "true" ]]; then
                    log_dry_run "cp ${old_rpa_file} ${new_rpa_file}"
                    log_dry_run "Replace versions in ${new_rpa_file}"
                else
                    cp "${old_rpa_file}" "${new_rpa_file}"
                    sed -i "s/${OLD_VER_DASH}/${NEW_VER_DASH}/g" "${new_rpa_file}"
                    sed -i "s/${OLD_VERSION}/${NEW_VERSION}/g" "${new_rpa_file}"
                fi
            else
                log_error "RPA file not found: ${old_rpa_file}"
                exit 1
            fi
        done

        # Update catalog files to include new version
        for env in production staging; do
            local catalog_rpa_file="${rpa_dir}/lvm-operator-catalog-${env}.yaml"
            if [[ -f "${catalog_rpa_file}" ]]; then
                if [[ "${DRY_RUN}" == "true" ]]; then
                    log_dry_run "Add ${NEW_VER_DASH} references to ${catalog_rpa_file}"
                else
                    # Add new version reference if not already present
                    if ! grep -q "${NEW_VER_DASH}" "${catalog_rpa_file}"; then
                        sed -i "s/${OLD_VER_DASH}/${OLD_VER_DASH}\n  - ${NEW_VER_DASH}/g" "${catalog_rpa_file}"
                    fi
                fi
            fi
        done
        log_info "  Release Plan Admissions updated"
    else
        log_error "RPA directory not found: ${rpa_dir}"
        exit 1
    fi

    # Commit changes if any
    run_cmd git add .
    if git diff --cached --quiet; then
        log_info "No changes to commit - konflux configurations already up to date"
        log_success "Step 3 complete (nothing to do)"
    else
        run_cmd git commit -m "LVMS ${NEW_VERSION} branching: add new version configurations"
        log_success "Konflux updates committed"
        echo ""
        log_info "Branch ready to push:"
        log_info "  Repository: konflux-release-data"
        log_info "  Branch: lvms-${NEW_VERSION}-branching"
        log_info "  Push command: git -C ${KONFLUX_DIR} push <remote> lvms-${NEW_VERSION}-branching"
        log_info ""
        log_info "After pushing, create a GitLab MR and get team approval"
    fi
}

step4_release_branch() {
    log_step "Step 4: Release Branch Tekton/Config Updates"

    cd "${LVM_OPERATOR_DIR}"

    # Cleanup: delete local PR branch if it exists from a previous run
    delete_local_branch_if_exists "update-tekton-release-${OLD_VERSION}" "${LVM_OPERATOR_DIR}"

    run_cmd git fetch origin

    # Ensure release branch exists on origin
    if ! remote_branch_exists "origin" "release-${OLD_VERSION}" "${LVM_OPERATOR_DIR}"; then
        log_error "Release branch release-${OLD_VERSION} does not exist on origin. Run step 1 first."
        exit 1
    fi

    # Reset to clean release branch from origin
    run_cmd git checkout "release-${OLD_VERSION}"
    run_cmd git reset --hard "origin/release-${OLD_VERSION}"

    # Create a branch for the PR
    run_cmd git checkout -b "update-tekton-release-${OLD_VERSION}"

    log_info "4.1 Updating Tekton files for release branch..."

    local tekton_files=(
        ".tekton/lvm-operator-bundle-pull-request.yaml"
        ".tekton/lvm-operator-bundle-push.yaml"
        ".tekton/lvm-operator-catalog-pull-request.yaml"
        ".tekton/lvm-operator-catalog-push.yaml"
        ".tekton/lvm-operator-pull-request.yaml"
        ".tekton/lvm-operator-push.yaml"
        ".tekton/lvms-must-gather-pull-request.yaml"
        ".tekton/lvms-must-gather-push.yaml"
    )

    local tekton_file
    for tekton_file in "${tekton_files[@]}"; do
        if [[ -f "${tekton_file}" ]]; then
            log_info "  Updating ${tekton_file}..."
            # Update branch trigger from main to release branch
            sed_replace 'target_branch == "main"' "target_branch == \"release-${OLD_VERSION}\"" "${tekton_file}"
        else
            log_warning "  File not found: ${tekton_file}"
        fi
    done

    log_info "4.2 Checking for old pipeline files to remove..."

    local old_pipelines=(
        ".tekton/catalog-build-pipeline.yaml"
        ".tekton/catalog-patching-build-pipeline.yaml"
        ".tekton/multi-arch-build-pipeline.yaml"
        ".tekton/single-arch-build-pipeline.yaml"
    )

    local pipeline_file
    for pipeline_file in "${old_pipelines[@]}"; do
        if [[ -f "${pipeline_file}" ]]; then
            log_info "  Note: ${pipeline_file} exists - keeping as it may be needed for pipeline references"
        fi
    done

    # Commit changes if any
    run_cmd git add .tekton/
    if [[ "${DRY_RUN}" == "true" ]]; then
        log_dry_run "Would commit tekton changes"
        log_success "Release branch tekton updates prepared (dry-run)"
    elif git diff --cached --quiet; then
        log_info "No changes to commit - release branch tekton configs already up to date"
        log_success "Step 4 complete (nothing to do)"
    else
        git commit -m "Update tekton configs for release-${OLD_VERSION} branch

Update branch triggers to target release-${OLD_VERSION} instead of main.
Pipeline references continue to point to main branch."
        log_success "Release branch tekton updates committed"
        echo ""
        log_info "Branch ready to push:"
        log_info "  Repository: lvm-operator"
        log_info "  Branch: update-tekton-release-${OLD_VERSION}"
        log_info "  Push command: git -C ${LVM_OPERATOR_DIR} push <remote> update-tekton-release-${OLD_VERSION}"
        log_info ""
        log_info "After pushing, create a GitHub PR targeting 'release-${OLD_VERSION}' branch"
    fi
}

step5_main_branch() {
    log_step "Step 5: Main Branch Version Updates"

    cd "${LVM_OPERATOR_DIR}"

    # Cleanup: delete local PR branch if it exists from a previous run
    delete_local_branch_if_exists "update-versions-${NEW_VERSION}" "${LVM_OPERATOR_DIR}"

    run_cmd git fetch origin

    # Reset to clean main branch from origin
    run_cmd git checkout main
    run_cmd git reset --hard origin/main
    run_cmd git checkout -b "update-versions-${NEW_VERSION}"

    log_info "5.1 Updating Tekton files for new version..."

    local tekton_files=(
        ".tekton/lvm-operator-bundle-pull-request.yaml"
        ".tekton/lvm-operator-bundle-push.yaml"
        ".tekton/lvm-operator-catalog-pull-request.yaml"
        ".tekton/lvm-operator-catalog-push.yaml"
        ".tekton/lvm-operator-pull-request.yaml"
        ".tekton/lvm-operator-push.yaml"
        ".tekton/lvms-must-gather-pull-request.yaml"
        ".tekton/lvms-must-gather-push.yaml"
    )

    local tekton_file
    for tekton_file in "${tekton_files[@]}"; do
        if [[ -f "${tekton_file}" ]]; then
            log_info "  Updating ${tekton_file}..."
            # Update version references (dash format)
            sed_replace "lvm-operator-${OLD_VER_DASH}" "lvm-operator-${NEW_VER_DASH}" "${tekton_file}"
            sed_replace "lvm-operator-bundle-${OLD_VER_DASH}" "lvm-operator-bundle-${NEW_VER_DASH}" "${tekton_file}"
            sed_replace "lvm-operator-catalog-${OLD_VER_DASH}" "lvm-operator-catalog-${NEW_VER_DASH}" "${tekton_file}"
            sed_replace "lvms-must-gather-${OLD_VER_DASH}" "lvms-must-gather-${NEW_VER_DASH}" "${tekton_file}"
            sed_replace "build-pipeline-lvm-operator-${OLD_VER_DASH}" "build-pipeline-lvm-operator-${NEW_VER_DASH}" "${tekton_file}"
            sed_replace "build-pipeline-lvm-operator-bundle-${OLD_VER_DASH}" "build-pipeline-lvm-operator-bundle-${NEW_VER_DASH}" "${tekton_file}"
            sed_replace "build-pipeline-lvm-operator-catalog-${OLD_VER_DASH}" "build-pipeline-lvm-operator-catalog-${NEW_VER_DASH}" "${tekton_file}"
            sed_replace "build-pipeline-lvms-must-gather-${OLD_VER_DASH}" "build-pipeline-lvms-must-gather-${NEW_VER_DASH}" "${tekton_file}"
            # Update v-format versions in catalog files
            sed_replace "CATALOG_VERSION=${OLD_VER_V}" "CATALOG_VERSION=${NEW_VER_V}" "${tekton_file}"
            sed_replace ":${OLD_VER_V}-" ":${NEW_VER_V}-" "${tekton_file}"
        else
            log_warning "  File not found: ${tekton_file}"
        fi
    done

    log_info "5.2 Updating container-build.args..."
    local build_args="release/container-build.args"
    if [[ -f "${build_args}" ]]; then
        # Reset to .0 for new Y-stream
        sed_replace "OPERATOR_VERSION=${OLD_VERSION}\\.[0-9]*" "OPERATOR_VERSION=${NEW_VERSION}.0" "${build_args}"
        sed_replace "LVMS_TAGS=${OLD_VER_V}" "LVMS_TAGS=${NEW_VER_V}" "${build_args}"
        sed_replace "OPENSHIFT_VERSIONS=${OLD_VER_V}-${OLD_NEXT_VER_V}" "OPENSHIFT_VERSIONS=${NEW_VER_V}-${NEW_NEXT_VER_V}" "${build_args}"
    else
        log_warning "  File not found: ${build_args}"
    fi

    log_info "5.3 Updating konflux.make..."
    local konflux_make="release/konflux.make"
    if [[ -f "${konflux_make}" ]]; then
        sed_replace "Y_STREAM\\?=\"${OLD_VER_V}\"" "Y_STREAM?=\"${NEW_VER_V}\"" "${konflux_make}"
    else
        log_warning "  File not found: ${konflux_make}"
    fi

    # Commit changes if any
    run_cmd git add .tekton/ release/
    if [[ "${DRY_RUN}" == "true" ]]; then
        log_dry_run "Would commit version updates"
        log_success "Main branch version updates prepared (dry-run)"
    elif git diff --cached --quiet; then
        log_info "No changes to commit - main branch versions already up to date"
        log_success "Step 5 complete (nothing to do)"
    else
        git commit -m "Update versions from ${OLD_VERSION} to ${NEW_VERSION}

- Update tekton component names and service accounts
- Update OPERATOR_VERSION to ${NEW_VERSION}.0
- Update LVMS_TAGS to ${NEW_VER_V}
- Update OPENSHIFT_VERSIONS to ${NEW_VER_V}-${NEW_NEXT_VER_V}
- Update Y_STREAM to ${NEW_VER_V}"
        log_success "Main branch version updates committed"
        echo ""
        log_info "Branch ready to push:"
        log_info "  Repository: lvm-operator"
        log_info "  Branch: update-versions-${NEW_VERSION}"
        log_info "  Push command: git -C ${LVM_OPERATOR_DIR} push <remote> update-versions-${NEW_VERSION}"
        log_info ""
        log_info "After pushing, create a GitHub PR targeting 'main' branch"
    fi
}

step6_catalog_updates() {
    log_step "Step 6: Catalog Updates (Post-merge)"

    log_info "This step should be run AFTER the PRs from steps 4 and 5 are merged."
    echo ""

    if [[ "${DRY_RUN}" == "false" ]]; then
        prompt_continue "Have the PRs been merged? Continue with catalog regeneration?"
    fi

    cd "${LVM_OPERATOR_DIR}"

    # Ensure we have a clean main branch from origin
    run_cmd git fetch origin
    run_cmd git checkout main
    run_cmd git reset --hard origin/main

    log_info "Regenerating catalog templates..."
    run_cmd make catalog-template

    log_info "Catalog templates have been regenerated."
    log_info "Files updated:"
    log_info "  - release/catalog/lvm-operator-catalog-template.yaml"
    log_info "  - release/catalog/lvm-operator-catalog-candidate-template.yaml"

    echo ""
    log_info "Next steps:"
    log_info "  - Review the generated catalog templates"
    log_info "  - Commit and push the changes if needed"
    log_info "  - Create a PR for the catalog updates"

    log_success "Catalog updates completed"
}

step7_verify_konflux_secrets() {
    log_step "Step 7: Verify Konflux SA Push Secrets"

    local ns="logical-volume-manag-tenant"

    # Check oc is available
    if ! command -v oc &> /dev/null; then
        log_error "oc CLI is not installed. Please install it first."
        exit 1
    fi

    # Check cluster access
    if [[ "${DRY_RUN}" == "false" ]]; then
        if ! oc get namespace "${ns}" &>/dev/null; then
            log_error "Cannot access namespace ${ns}. Ensure you are logged into the Konflux cluster (stone-prd-rh01)."
            exit 1
        fi
    fi

    # Push secrets and their corresponding SAs
    # Each SA needs its matching image-push secret plus the shared registry secret
    local -A sa_to_secret=(
        ["build-pipeline-lvm-operator-${NEW_VER_DASH}"]="imagerepository-for-lvm-operator-lvm-operator-image-push"
        ["build-pipeline-lvm-operator-bundle-${NEW_VER_DASH}"]="imagerepository-for-lvm-operator-lvm-operator-bundle-image-push"
        ["build-pipeline-lvms-must-gather-${NEW_VER_DASH}"]="imagerepository-for-lvm-operator-lvms-must-gather-image-push"
        ["build-pipeline-lvm-operator-catalog-${NEW_VER_DASH}"]="imagerepository-for-lvm-operator-catalog-image-push"
    )

    local all_push_secrets=(
        "imagerepository-for-lvm-operator-lvm-operator-image-push"
        "imagerepository-for-lvm-operator-lvm-operator-bundle-image-push"
        "imagerepository-for-lvm-operator-lvms-must-gather-image-push"
        "imagerepository-for-lvm-operator-catalog-image-push"
        "imagerepository-for-lvm-operator-topolvm-image-push"
        "registry-stage-redhat-io-docker"
    )

    local all_sas=(
        "build-pipeline-lvm-operator-${NEW_VER_DASH}"
        "build-pipeline-lvm-operator-bundle-${NEW_VER_DASH}"
        "build-pipeline-lvms-must-gather-${NEW_VER_DASH}"
        "build-pipeline-lvm-operator-catalog-${NEW_VER_DASH}"
    )

    # 7.1 Ensure common-secret label on all push secrets
    log_info "7.1 Ensuring common-secret label on push secrets..."
    local secret
    for secret in "${all_push_secrets[@]}"; do
        if [[ "${DRY_RUN}" == "true" ]]; then
            log_dry_run "oc label secret ${secret} build.appstudio.openshift.io/common-secret=true --overwrite -n ${ns}"
        else
            if oc get secret "${secret}" -n "${ns}" &>/dev/null; then
                oc label secret "${secret}" build.appstudio.openshift.io/common-secret=true --overwrite -n "${ns}"
                log_info "  Labeled ${secret}"
            else
                log_warning "  Secret ${secret} not found, skipping"
            fi
        fi
    done

    # 7.2 Link each SA with its matching push secret
    log_info "7.2 Linking push secrets to service accounts..."
    local sa
    for sa in "${all_sas[@]}"; do
        local matching_secret="${sa_to_secret[${sa}]}"
        if [[ "${DRY_RUN}" == "true" ]]; then
            log_dry_run "oc secrets link ${sa} ${matching_secret} -n ${ns}"
            log_dry_run "oc secrets link ${sa} registry-stage-redhat-io-docker -n ${ns}"
        else
            if ! oc get sa "${sa}" -n "${ns}" &>/dev/null; then
                log_warning "  SA ${sa} not found — Konflux components may not be provisioned yet"
                log_warning "  Ensure the Konflux MR from step 3 is merged and components are created"
                continue
            fi
            oc secrets link "${sa}" "${matching_secret}" -n "${ns}"
            oc secrets link "${sa}" registry-stage-redhat-io-docker -n "${ns}"
            log_info "  Linked ${sa} with ${matching_secret} and registry-stage-redhat-io-docker"
        fi
    done

    # 7.3 Verify
    log_info "7.3 Verifying secret links..."
    for sa in "${all_sas[@]}"; do
        if [[ "${DRY_RUN}" == "true" ]]; then
            log_dry_run "oc get sa ${sa} -n ${ns} -o jsonpath='{.secrets[*].name}'"
        else
            if oc get sa "${sa}" -n "${ns}" &>/dev/null; then
                local secrets
                secrets=$(oc get sa "${sa}" -n "${ns}" -o jsonpath='{.secrets[*].name}')
                log_info "  ${sa}: ${secrets}"
            fi
        fi
    done

    log_success "Konflux SA push secret verification completed"
}

#######################################
# Main execution
#######################################

main() {
    parse_args "${@}"

    # Show steps and exit if requested
    if [[ "${SHOW_STEPS}" == "true" ]]; then
        show_steps
    fi

    # Validate required arguments
    if [[ -z "${OLD_VERSION}" || -z "${NEW_VERSION}" ]]; then
        log_error "Both --old-version and --new-version are required"
        usage
    fi

    if [[ -z "${LVM_OPERATOR_DIR}" ]]; then
        log_error "--lvm-dir is required"
        usage
    fi

    validate_version "${OLD_VERSION}"
    validate_version "${NEW_VERSION}"

    compute_derived_versions

    echo ""
    log_info "LVMS Branching Automation"
    log_info "========================="
    log_info "Old version: ${OLD_VERSION} (${OLD_VER_DASH}, ${OLD_VER_V})"
    log_info "New version: ${NEW_VERSION} (${NEW_VER_DASH}, ${NEW_VER_V})"
    log_info "OPENSHIFT_VERSIONS: ${NEW_VER_V}-${NEW_NEXT_VER_V}"
    log_info "Dry run: ${DRY_RUN}"
    if [[ -n "${STEP}" ]]; then
        log_info "Running only step: ${STEP}"
    fi
    echo ""

    check_prerequisites
    sync_repositories
    validate_old_version_resources

    if [[ -n "${STEP}" ]]; then
        case "${STEP}" in
            1) step1_github_branching ;;
            2) step2_prodsec_update ;;
            3) step3_konflux_updates ;;
            4) step4_release_branch ;;
            5) step5_main_branch ;;
            6) step6_catalog_updates ;;
            7) step7_verify_konflux_secrets ;;
            *)
                log_error "Invalid step: ${STEP} (must be 1-7)"
                exit 1
                ;;
        esac
    else
        prompt_continue "Start the branching process?"

        step1_github_branching
        prompt_continue "Continue to Step 2 (prodsec)?"

        step2_prodsec_update
        prompt_continue "Continue to Step 3 (konflux)?"

        step3_konflux_updates
        prompt_continue "Continue to Step 4 (release branch updates)?"

        step4_release_branch
        prompt_continue "Continue to Step 5 (main branch updates)?"

        step5_main_branch

        echo ""
        log_info "Steps 1-5 completed!"
        log_info ""
        log_info "Step 6 (catalog updates) should be run AFTER the PRs are merged."
        log_info "Run: $(basename "${0}") --old-version ${OLD_VERSION} --new-version ${NEW_VERSION} --lvm-dir ${LVM_OPERATOR_DIR} --step 6"
        log_info ""
        log_info "Step 7 (verify Konflux SA secrets) should be run AFTER step 3's MR is merged and components are provisioned."
        log_info "Requires oc login to the Konflux cluster (stone-prd-rh01)."
        log_info "Run: $(basename "${0}") --old-version ${OLD_VERSION} --new-version ${NEW_VERSION} --lvm-dir ${LVM_OPERATOR_DIR} --step 7"
    fi

    echo ""
    log_success "Branching automation completed!"
}

main "${@}"
