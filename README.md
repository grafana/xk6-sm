# xk6-traces

A [k6](https://k6.io) extension that instruments `k6/http` requests with
[OpenTelemetry](https://opentelemetry.io) distributed traces: one span per
HTTP request, propagated downstream via a standard W3C `traceparent` header,
optionally attached under an externally supplied parent trace.

It reuses k6's own OTel export pipeline entirely (`--traces-output=otel=...`)
— this extension only creates spans; it never configures an exporter itself.

## Requirements

- [`xk6`](https://github.com/grafana/xk6) to build the custom binary.
- A local checkout of `go.k6.io/k6/v2` (this repo is built with a `replace`
  directive against a local k6 checkout, not a published release — adjust
  the path below to wherever yours lives).

## Building

```sh
xk6 build \
  --with github.com/grafana/xk6-http-traces=. \
  --replace go.k6.io/k6/v2=../k6 \
  -o build/k6-traces
```

This produces a standalone binary at `build/k6-traces`. It does not modify
or replace any other k6 binary on your system.

## Quick start

```js
import http from 'k6/http';
import 'k6/x/tracing';

export default function () {
  http.get('https://example.com');
}
```

```sh
./build/k6-traces run --traces-output="otel=http://127.0.0.1:4318/v1/traces,proto=http" script.js
```

See [`examples/basic.js`](examples/basic.js) for a fuller walkthrough
(manual spans, reading the current traceparent, `K6_TRACE_PARENT`).

## Automatic instrumentation

Importing `k6/x/tracing` is enough — every `http.get/post/...` call in every
VU is instrumented from the very first iteration, with no script-side setup
call required. Each VU subscribes to k6's `IterStart` event
(`go.k6.io/k6/v2/event`) and installs the tracing `RoundTripper` on
`state.Transport` before that event's wait completes, which k6 guarantees
happens before the VU's iteration function runs.

This relies on `go.k6.io/k6/v2/event` being a **public** package, so external
modules can import the event types without reaching into k6's `internal/`
tree. That's only true today on the local `../k6` checkout this extension is
built against (branch `mem/split-event-package`) — it isn't part of any
released k6 version, and hasn't landed on k6's upstream `main` yet. Until (or
unless) that change is upstreamed, this extension is coupled to that
specific checkout.

## JS API

Import as `import tracing from 'k6/x/tracing';`.

| Call | Description |
|---|---|
| `tracing.startSpan(name, attrs)` | Starts a manual span as a child of the VU's current trace context, for instrumenting non-HTTP work (validation steps, waits, custom timing). `attrs` is a flat object of string key/value pairs set as span attributes. Returns a span handle, or `null` if called before any VU state exists. |
| `span.setAttribute(key, value)` | Sets one additional string attribute on a span returned by `startSpan`. |
| `span.end(errMsg)` | Ends the span. Pass `''` for a normal end, or a non-empty string to mark the span as failed with that error message. |
| `tracing.currentTraceparent()` | Returns the current W3C `traceparent` value for this VU's trace context — the externally supplied parent if `K6_TRACE_PARENT` is set, otherwise this VU's own root span once its first request has been made. Returns `''` before that, or whenever `--traces-output` isn't configured (see below). Useful for forwarding trace context to something this extension doesn't intercept (a `k6/net/grpc` call, log correlation, etc). |

## Environment variables

| Variable | Description |
|---|---|
| `K6_TRACE_PARENT` | A W3C `traceparent` value (e.g. `00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01`) identifying an externally supplied parent span. When set, **every VU's requests become children of that one span**, producing a single trace for the whole k6 run — useful for slotting a k6 run into a larger trace started by something upstream (a CI pipeline, an orchestrator). If unset, empty, or malformed, it's ignored (logged as a warning if malformed) and each VU gets its own trace instead — see below. |

## Running k6 with `--traces-output=otel`

This flag is k6 core's, not this extension's — the extension only calls
`state.TracerProvider.Tracer(...)`, which is wired up by whatever you pass
here. Full syntax:

```
--traces-output=otel[=<url>][,proto=<http|grpc>][,header.<Name>=<value>]...
```

- **No value** (`--traces-output=otel`): defaults to gRPC at
  `127.0.0.1:4317`, no TLS.
- **With an endpoint**: the value after `otel=` must be a full URL,
  including scheme (`http://` or `https://`) — a bare `host:port` (no
  scheme) will fail to parse. The URL's scheme also sets whether the
  connection is made without TLS (`http://` → insecure, `https://` → TLS).
- **`proto=http`**: OTLP over HTTP. Include the collector's ingest path in
  the URL, typically `/v1/traces`.
- **`proto=grpc`** (default): OTLP over gRPC. The URL must have no path
  component when using gRPC.
- **`header.<Name>=<value>`**: adds a custom header to every export
  request (repeatable) — commonly used for an auth token. `<Name>` is used
  **verbatim as the literal HTTP header name** — it's not normalized or
  validated against anything, so `header.Authentication=...` sends a header
  literally named `Authentication`, which most backends will silently
  ignore while still rejecting the request as unauthenticated (they're
  looking for `Authorization`). Double-check this matches what your
  collector/backend actually expects. Spaces in `<value>` are preserved
  as-is (e.g. `Bearer <token>` works); only literal commas are off-limits,
  since `,` is the separator between `otel=...`/`proto=...`/`header...`
  parts.

Examples:

```sh
# Local OTLP/gRPC collector on the default port
./build/k6-traces run --traces-output=otel script.js

# OTLP/HTTP, explicit endpoint and path
./build/k6-traces run --traces-output="otel=http://127.0.0.1:4318/v1/traces,proto=http" script.js

# OTLP/gRPC, custom host, with an auth header
./build/k6-traces run --traces-output="otel=http://otel-collector.example.com:4317,proto=grpc,header.Authorization=Bearer xyz-token" script.js

# Slot this run into an externally started trace
K6_TRACE_PARENT="00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" \
  ./build/k6-traces run --traces-output="otel=http://127.0.0.1:4318/v1/traces,proto=http" script.js
```

If `--traces-output` isn't set at all, k6 uses a no-op tracer: this
extension still runs safely, but spans get no real trace/span IDs, so no
`traceparent` header is injected and `tracing.currentTraceparent()` always
returns `''`. There's no error and no meaningful overhead in this mode — the
SDK-level span/ID generation is what's skipped, not just the network export.

If `--traces-output=otel` **is** set but the endpoint is unreachable, the run
itself still completes normally, but k6 may take noticeably longer to exit
while the OTel SDK's batch span processor tries (and times out) flushing
spans on shutdown.

## Trace/span model

- **No `K6_TRACE_PARENT`**: each VU's first HTTP request becomes that VU's
  own root span; every later request in the same VU becomes a child of it.
  Result: one independent trace per VU, distinguishable via the `k6.vu.id`
  attribute. There's no shared span whose lifecycle needs managing — the
  "root" is just an ordinary request span, ended normally like any other.
- **With `K6_TRACE_PARENT`**: every VU's requests become children of that
  one external span instead, forming a single trace across the whole run
  regardless of VU count.
- **Span naming**: `<METHOD> <host>` (e.g. `GET example.com`), not the full
  URL, to avoid unbounded span-name cardinality from path/query variation.
- **Redirects and `http.batch()`**: each hop/request gets its own span,
  correctly parented.
- **Span timing**: coarse-grained — span duration is the request's
  round-trip wall time. k6's own detailed per-phase timings (DNS, connect,
  TLS handshake, send, wait, receive) aren't exposed as span events in this
  version.

## Span attributes

Standard OTel HTTP client attributes ([semconv
v1.24.0](https://github.com/open-telemetry/semantic-conventions)):

| Attribute | Description |
|---|---|
| `http.request.method` | HTTP method |
| `url.full` | Full request URL, **including the query string as-is** — see the note below |
| `server.address` | Request host |
| `server.port` | Request port, if explicit in the URL |
| `http.response.status_code` | Response status code |

Custom `k6.*` attributes (matching the tag vocabulary k6's own cloud
insights collector uses, for consistency):

| Attribute | Description |
|---|---|
| `k6.vu.id` | The VU ID that made the request |
| `k6.iteration` | The iteration number within that VU |
| `k6.scenario` | The active scenario name, if any |
| `k6.group` | The active `group()` name, if any |

> **Note on `url.full`**: it's captured verbatim, including query strings.
> Real-world URLs often carry API keys, session tokens, or other sensitive
> data in query parameters — this extension does not redact or scrub
> `url.full` before it's sent to whatever backend `--traces-output` points
> at. Be mindful of this if that backend is third-party or shared, and
> consider stripping sensitive query parameters from URLs in your script
> before making the request if this is a concern.

## Known limitations

- **WebSocket traffic isn't instrumented.** WebSocket upgrades use a
  separate connection path, not the wrapped HTTP transport.
- **No sampling or rate-limiting controls.** Every instrumented request
  creates a span; at high request rates this adds allocation and export
  overhead with no way to sample down from within the extension.
- **Depends on a k6-internal field.** This extension works by wrapping
  `lib.State.Transport`, which k6 core has flagged (in-code) as likely to
  move into the `k6/http` module in a future refactor
  ([grafana/k6#2293](https://github.com/grafana/k6/issues/2293)). If that
  lands, this extension may need updating — most likely a loud build
  failure rather than a silent behavior change, but worth knowing.
