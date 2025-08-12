# xk6-sm

> [!WARNING]
> Heads up, Synthetic Monitoring users: You do not need to build or download this. A k6 binary compiled with this extension is already shipped in our [agent](https://github.com/grafana/synthetic-monitoring-agent) packages.

Output k6 extension used by the [synthetic monitoring agent](https://github.com/grafana/synthetic-monitoring-agent).

## Configuration

By default, this extension will drop all metrics which have a `resource_type` tag when the value for the tag is not `Document`. This is done to avoid generating metrics for every URL that a k6 script using browser would normally generate, including those for images, scripts, and others.

This `resource_type` allowlist is configurable by means of the `SM_K6_BROWSER_RESOURCE_TYPES` environment variable, which should be set to a comma-separated list of `resource_type`s that should not be dropped. The list of known `resource_type`s can be found [here](https://github.com/grafana/k6/blob/v0.57.0/internal/js/modules/k6/browser/common/http.go#L25).

This matching is case-insensitive, and the special value `*` will cause every `resource_type` to be retained. Conversely, an empty list will cause all metrics from browser to be omitted.

## Build

Type `make` (or `make build-native` if you just want a binary for your current
operating system / architecture). The resulting binaries will be found in the
`dist/` directory.

The extension is built using [xk6](https://github.com/grafana/xk6).

## Release process

Merge the release PR created by release-please. Once a release is created in github, a CI/CD pipeline will build the artifacts and attach them to the release.
