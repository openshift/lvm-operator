catalog:
	opm alpha render-template semver -o yaml catalog-template.yaml > konflux-catalog/lvm-operator-catalog.yaml

.PHONY: verify
verify:
	opm validate konflux-catalog