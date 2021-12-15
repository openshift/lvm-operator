OCS must-gather
=================

`lvm-must-gather` is a tool built on top of [OpenShift must-gather](https://github.com/openshift/must-gather)
that expands its capabilities to gather LVM Operator information.

### Usage
```sh
oc adm must-gather --image=quay.io/ocs-dev/lvm-must-gather
```

The command above will create a local directory with a dump of the lvm state.

You will get a dump of:
- The LVM Operator namespaces (and its children objects)
- All namespaces (and their children objects) that belong to any LVM resources
- All LVM CRD's definitions


### How to Contribute

#### Contribution Flow
Developers must follow these steps to make a change:
1. Fork the `red-hat-storage/lvm-operator` repository on GitHub.
2. Create a branch from the master branch, or from a versioned branch (such
   as `release-4.2`) if you are proposing a backport.
3. Make changes.
4. Ensure your commit messages include sign-off.
5. Push your changes to a branch in your fork of the repository.
6. Submit a pull request to the `red-hat-storage/lvm-operator` repository.
7. Work with the community to make any necessary changes through the code
   review process (effectively repeating steps 3-7 as needed).

#### Commit and Pull Request Messages

- Follow the standards mentioned in [LVM-Operator How to Contribute](./../CONTRIBUTING.md)
- Tag the Pull Request with `must-gather`
