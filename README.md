# LVM Operator Catalog
This catalog is used for internal Redhat use. It is used in our build and release pipelines and not meant external use.
## Background
We make use of the OLM File Based Catalog [Semver Templates](https://olm.operatorframework.io/docs/reference/catalog-templates/#semver-template) for managing our catalog. This makes it easy to add and update new versions of the Operator and generate the resulting catalog.

## Workflows
### Prerequisites
You will need the following to be installed:
- OPM - [OPM Github Releases](https://github.com/operator-framework/operator-registry/releases)
- [Skopeo](https://github.com/containers/skopeo/blob/main/install.md)
- Openshift CLI

### Adding newly released images
In order to add newly released images, you will need to update the template files located in `./templates`.
You can run `TARGET_CATALOGS="v4.20" make templates` to regenerate a single template or specify multiple with space delineation `TARGET_CATALOGS="v4.19 v4.20" make templates`. You can also run `make templates` without any `TARGET_CATALOGS` specified which will generate catalogs for every y-stream and add all related z-stream tags to the template file. By default, the script will add the operator versions in line with our support matrix. This means that the catalog for **v4.18** will have operator references for **v4.17.z** and **v4.18.z**.
Once the template has been updated you can either run a single catalog update or run an update for all catalogs. It is recommended to run the single catalog update if there are only a few updates as the update for all catalogs can take a long time.

> **NOTE** you will need to run the `make templates` command for any catalog updates

### Update a single catalog
You can update a single catalog by providing the operator version you wish to update
```bash
$ CATALOG_VERSION="v4.18" make catalog
```
This will use the corresponding template (`templates/lvm-operator-catalog-v4.18-template.yaml`) to generate the catalog (`catalogs/lvm-operator-catalog-v4.18`)

### Update all catalogs
In order to update all catalogs from their respective template, you will need to run `make catalogs`. You can also run `make all` which will update the templates and catalogs in one go.

### Catalog docker image
If you want to generate your own catalog image for testing, you can build the `konflux-catalog.Dockerfile` containerfile which will produce a file based catalog image. You will need to build with the `CATALOG_VERSION` argument specified in order to specify the correct catalog to be copied into the image.
```bash
$ podman build --build-arg=CATALOG_VERSION="v4.18" -t lvm-operator-catalog:v4.18 -f konflux-catalog.Dockerfile
```

### Validate a catalog
OPM Validation will happen as part of the dockerfile build. If you would like to manually run a validation, you will need to copy the target catalog to it's own folder and run `bin/opm validate {{ NEW FOLDER }}`.

### Testing a catalog with released or unreleased operator bundles
> In order to test a catalog, you will need to have your KUBECONFIG environment variable set to a valid kubeconfig. You will also need to have your registry credentials configured in `$HOME/.docker/config.json`

> **Note:** if you are testing a catalog that was built by konflux or otherwise already exists, skip to step 4

1. Create or update one of the catalog templates in `./templates` to contain your custom operator bundle pull spec (likely a quay link). It is recommended to use the `candidate` channel for this and create a custom version number such as `v4.test`
2. Generate the catalog based on that template by running
  ```bash
  # Note that this would generate a catalog based off templates/lvm-operator-catalog-v4.test-template.yaml
  $ CATALOG_VERSION="v4.test" make catalog # This will create catalogs/lvm-operator-catalog/v4.test.json
  ```
3. Build the catalog container and push it to your quay registry
  ```bash
  $ podman build --build-arg=CATALOG_VERSION="v4.test" -t quay.io/path/to/your/repo/lvm-operator-catalog:v4.test -f konflux-catalog.Dockerfile
  ```
4. Generate the required manifests to test the catalog
  ```bash
  $ export CATALOG_SOURCE_IMAGE=quay.io/path/to/your/repo/lvm-operator-catalog # This should be the path to your custom image, defaults to konflux built quay images
  $ export CATALOG_VERSION=v4.test # Change to match whatever version you use
  $ export OPERATOR_CHANNEL=candidate # Change to match the channel you set in the template if using a custom catalog
  $ make test-manifests
  ```
  > **Note:** if overwriting an existing version with a custom operator, you will likely need to update `manifests/imagedigestmirrors.yaml` to include your quay repo as a mirror. If you create a unique version and custom catalog, you should not need to update the mirrors.
5. Update the cluster pull secret (if using images from locations other than `registry.redhat.io`)
  ```bash
  $ make update-cluster-pull-secret
  ```
6. Apply the manifests to the cluster
  ```bash
  $ oc apply -R -f manifests
  ```

Once the manifests have been applied, you can monitor the operator install using the oc CLI or the cluster console
