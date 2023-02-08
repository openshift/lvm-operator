# How to Contribute

The lvm-operator project is under the [Apache 2.0 license](LICENSE). We accept
contributions via GitHub pull requests. This document outlines how to
contribute to the project.

## Contribution Flow

Developers must follow these steps to make a change:

1. Fork the `openshift/lvm-operator` repository on GitHub.
2. Create a branch from the `main` branch, or from a versioned branch (such
   as `release-4.9`) if you are proposing a backport.
3. Make changes.
4. Create tests as needed and ensure that all tests pass.
5. Push your changes to a branch in your fork of the repository.
6. Submit a pull request to the `openshift/lvm-operator` repository.
7. Work with the community to make any necessary changes through the code
   review process (effectively repeating steps 3-7 as needed).


## Developer Environment Installation
ToDo


## Commits Per Pull Request

Pull requests should always represent a complete logical change. Where
possible, though, pull requests should be composed of multiple commits that
each make small but meaningful changes. Striking a balance between minimal
commits and logically complete changes is an art as much as a science, but
when it is possible and reasonable, divide your pull request into more commits.

Some times when it will almost always make sense to separate parts of a change
into their own commits are:
- Changes to unrelated formatting and typo-fixing.
- Refactoring changes that prepare the codebase for your logical change.

Even when breaking down commits, each commit should leave the codebase in a
working state. The code should add necessary unit tests and pass unit tests,
formatting tests, and usually functional tests. There can be times when
exceptions to these requirements are appropriate (for instance, it is sometimes
useful for maintainability to split code changes and related changes to CRDs
and CSVs). Unless you are very sure this is true for your change, though, make
sure each commit passes CI checks as above.

Make sure to update the bundle manifests after making changes, in a separate
commit using:
```
make bundle
```

## Commit structure

LVM Operator maintainers value clear and explanatory commit messages. So by default
each of your commits must follow the rules below:

### We follow the common commit conventions
```
type: subject

body?

footer?
```

### Here is an example of an acceptable commit message for a bug fix:
```
component: commit title

This is the commit message, where I'm explaining what the bug was, along
with its root cause.
Then I'm explaining how I fixed it.

Fix: https://bugzilla.redhat.com/show_bug.cgi?id=<NUMBER>

Signed-off-by: First_Name Last_Name <email address>
```

### Here is an example of an acceptable commit message for a new feature:
```
component: commit title

This is the commit message, here I'm explaining, what this feature is
and why do we need it.

Signed-off-by: First_Name Last_Name <email address>
```


### More Guidelines:
- Type/component should not be empty.
- Your commit msg should not exceed more than 72 characters per line.
- Header should not have a full stop.
- Body should always end with the full stop.
- There should be one blank line in b/w header and body.
- There should be one blank line in b/w body and footer.
- Your commit message must be signed-off.
- *Recommendation*: A "Co-authored-by:" line should be added for each
  additional author.
