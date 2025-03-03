# xk6-sm

> [!WARNING]
> Heads up, Synthetic Monitoring users: You do not need to build or download this. A k6 binary compiled with this extension is already shipped in our [agent](https://github.com/grafana/synthetic-monitoring-agent) packages.

Output k6 extension used by the [synthetic monitoring agent](https://github.com/grafana/synthetic-monitoring-agent).

## Build

Use [xk6](https://github.com/grafana/xk6). See the CI/CD pipelines for a full example of a build command.

## Release process

Merge the release PR created by release-please. Once a release is created in github, a CI/CD pipeline will build the artifacts and attach them to the release.
