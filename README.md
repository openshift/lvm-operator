# LVM Operator Catalog
This catalog is used for internal Redhat use. It is used in our build and release pipelines and not meant external use. 

## Workflows
### Prerequisites
You will need to have OPM installed on your machine in order to run the generate and validate commands. OPM can be installed from the Operator Registry [Github Releases](https://github.com/operator-framework/operator-registry/releases)

### Catalog Updates
We make use of the OLM File Based Catalog [Semver Templates](https://olm.operatorframework.io/docs/reference/catalog-templates/#semver-template) for managing our catalog. This makes it easy to add and update new versions of the Operator and generate the resulting catalog.

To add a new version to the catalog, edit `catalog-template.yaml` and add the new version to the proper channel. The channels we use are as follows:
- **Candidate** for pre-release and unsupported operator bundles (engineering builds)
- **Stable** for any operator bundles that have been officially released to registry.redhat.com and are fully supported

Once the new version has been added to the template, you can generate a new catalog by running `make catalog` which will put the new file based catalog in the konflux-catalog folder. You can then commit your changes to github where Konflux will pick up the changes and generate a new catalog image.

If you want to generate your own catalog image for testing, you can build the `konflux-catalog.Dockerfile` containerfile which will produce a file based catalog image.

To validate the catalog is formatted properly, run `make verify`.