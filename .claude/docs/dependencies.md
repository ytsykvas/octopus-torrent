# Adding or upgrading a dependency

The policy is the owner's decision and is not negotiable:
[toolchain-versions](../../docs/decisions/toolchain-versions.md). In short — packages (npm and Go
modules, including transitive ones): the latest stable version published **at least 14 days ago**, no
pre-releases; toolchains (Go, Node.js): patch releases immediately, new minor/major lines after 14
days. Nothing refuses a command before it runs: `frontend/.npmrc` (`min-release-age=14`) and the
quality gate (`go tool devtool check`) are the checks, and these rules are what keeps a younger
version from running at all.

## 1. Survey
```bash
go tool devtool check deps        # "updates" lists versions out of quarantine for 14 days
npm --prefix frontend outdated    # the raw lists, if needed
go list -m -u all
```

## 2. Choose the version

For each candidate, read the publish time and take the newest stable one at least 14 days old:
```bash
npm --prefix frontend view <pkg> time --json
curl -s https://proxy.golang.org/<escaped module path>/@v/<version>.info
```
(Module paths escape capitals as `!` + lowercase.) Note the date — it goes into the ADR table.

## 3. Apply — exact versions only
```bash
npm --prefix frontend install --save-exact <pkg>@<version>
go get <module>@<version>
go get -tool <module>/cmd/<tool>@<version>
```
Never `@latest`, `-u`, `npx <pkg>`, global installs, or a lowered `min-release-age`. A new Go import
is added with `go get module@vX.Y.Z` **before** building: `go mod tidy` and `-mod=mod` would resolve it
to the latest release, so do not use them. Changing an `.npmrc` always goes to the owner. A package that
needs an install script is added to `allowScripts` in `frontend/package.json` only with a stated
reason. Transitive Go modules stay where Go's MVS puts them unless one has a known vulnerability.

## 4. Verify
```bash
go tool devtool check deps        # release age of every locked version, npm audit, govulncheck
```
Then the full [`/check`](../commands/check.md) (`go tool devtool check`), and `/qa` if the change can
affect the `.app`.

## 5. Record

Update the "Current set" table (version and release date) and, if relevant, the quarantine list in
the ADR, in the same commit. Commit a dependency bump separately from code changes it forces.
