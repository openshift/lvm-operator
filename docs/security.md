# LVM Operator Security Docs
## Running Snyk
In order to run Snyk against the code, you will need access to the `Logical Volume Manager Storage (LVMS)` organization in Snyk. There are a few options for authentication but the preferred approach is to use an api token and then run the desired analysis command.

```bash
$ export SNYK_TOKEN=<your-snyk-token> make vuln-scan-code
```

You can also authenticate using `$ snyk auth` which will do browser authentication. See [the snyk auth docs](https://docs.snyk.io/snyk-cli/authenticate-the-cli-with-your-account) for more information.

### Available analysis commands
There are several commands in the make file to make security scanning easier, particularly for CI. The available commands are as follows:

`make vuln-scan-code`
  - This will run static analysis against the code source code in the repository

`make vuln-scan-deps`
  - This will scan dependencies in the `go.mod` file for known security vulnerabilities

`make vuln-scan-container`
  - This will scan a container for vulnerabilities. The `IMAGE_REPO` and `IMAGE_TAG` variables can be overwritten to scan different images.
