# LVM Operator Catalog
This catalog is used for internal Redhat use. It is used in our build and release pipelines and not meant external use.
## Background
We make use of the OLM File Based Catalog [Semver Templates](https://olm.operatorframework.io/docs/reference/catalog-templates/#semver-template) for managing our catalog. This makes it easy to add and update new versions of the Operator and generate the resulting catalog.

## Workflows
### Prerequisites
You will need to have OPM installed on your machine in order to run the generate and validate commands. OPM can be installed from the Operator Registry [Github Releases](https://github.com/operator-framework/operator-registry/releases)

You will also need [skopeo](https://github.com/containers/skopeo/blob/main/install.md) installed and docker credentials configured for `registry.redhat.io`

### Adding newly released images
In order to add newly released images, you will need to update the template files located in `./templates`.
You can run `make templates` which will automatically poll the released images via skopeo and generate catalogs for each released y-stream and add all related
z-stream tags to the template file. By default, the script will add the operator versions in line with our support matrix. This means that the catalog for **v4.18** will have operator references for **v4.17.z** and **v4.18.z**.
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
