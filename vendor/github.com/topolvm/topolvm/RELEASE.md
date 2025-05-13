Release procedure
=================

This document describes how to release a new version of TopoLVM.

Versioning
----------

Follow [semantic versioning 2.0.0][semver] to choose the new version number.

The format of release notes
---------------------------

In the release procedure for both the app and Helm Chart, the release note is generated automatically,
and then it is edited manually. In this step, PRs should be classified based on [Keep a CHANGELOG](https://keepachangelog.com/en/1.1.0/).

The result should look something like:

```markdown
## What's Changed

### Added

* Add a notable feature for users (#35)

### Changed

* Change a behavior affecting users (#33)

### Removed

* Remove a feature, users action required (#39)

### Fixed

* Fix something not affecting users or a minor change (#40)
```

Bump version
------------

1. Check if a new release is required by [checking the differences](https://github.com/topolvm/topolvm/compare/vX.Y.Z...main) since the last release. If so, determine a new version number and define the `VERSION` variable.

    ```console
    VERSION=1.2.3
    ```

2. Go to [the rule setting page](https://github.com/topolvm/topolvm/settings/rules/2151135) and change the value of "Enforcement status" to `Active`.

3. Add a new tag and push it.

    ```console
    git switch main
    git pull
    git tag v$VERSION
    git push origin v$VERSION
    ```

4. Once a new tag is pushed, [GitHub Actions][] automatically
   creates a draft release note for the tagged version,
   builds a tar archive for the new release,
   and attaches it to the release note.
   
   Visit https://github.com/topolvm/topolvm/releases to check
   the result. 

5. Edit the auto-generated release note
   and remove PRs which contain changes only to the helm chart.
   Then, publish it.

Bump Chart Version
------------------

TopoLVM Helm Chart will be released independently.
This will prevent the TopoLVM version from going up just by modifying the Helm Chart.

1. If necessary, go to [the rule setting page](https://github.com/topolvm/topolvm/settings/rules/2151135) and change the value of "Enforcement status" to `Active`.
   - If you are here after releasing the app, you should already have this setting changed.

2. Determine a new version number by [checking the differences](https://github.com/topolvm/topolvm/compare/topolvm-chart-vX.Y.Z...main) since the last release. Then, manually run the workflow to create a PR to update the Helm Chart.

   https://github.com/topolvm/topolvm/actions/workflows/create-chart-update-pr.yaml

3. Review and merge the auto-created PR.
   - Before merging, go to [the rule setting page](https://github.com/topolvm/topolvm/settings/rules/2151135) and change the value of "Enforcement status" to `Disabled`.

4. Manually run the GitHub Actions workflow for the release.

   https://github.com/topolvm/topolvm/actions/workflows/helm-release.yaml

   When you run workflow, [helm/chart-releaser-action](https://github.com/helm/chart-releaser-action) will automatically create a GitHub Release.

5. Edit the auto-generated release note as follows:
   1. Select the "Previous tag", which is in the form of "topolvm-chart-vX.Y.Z".
   2. Clear the textbox, and click "Generate release notes" button.
   3. Remove PRs which do not contain changes to the helm chart.

[semver]: https://semver.org/spec/v2.0.0.html
[example]: https://github.com/cybozu-go/etcdpasswd/commit/77d95384ac6c97e7f48281eaf23cb94f68867f79
[GitHub Actions]: https://github.com/topolvm/topolvm/actions
