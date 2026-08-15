// Example: instrumenting k6/http requests with OpenTelemetry traces.
//
// Build the custom binary first:
//
//   cd xk6-traces
//   xk6 build --with github.com/grafana/xk6-http-traces=. \
//     --replace go.k6.io/k6/v2=../k6 -o build/k6-traces
//
// Run it against a real OTLP backend (e.g. an OTel Collector or Jaeger
// listening for OTLP/HTTP on :4318):
//
//   ./build/k6-traces run --traces-output=otel=http://127.0.0.1:4318/v1/traces examples/basic.js
//
// Without --traces-output, requests are still instrumented, but k6's
// TracerProvider is a no-op: spans get no real trace/span IDs, so no
// traceparent header is injected and tracing.currentTraceparent() returns ""
// - zero overhead, zero errors, just nothing to see. A real backend (even
// one --traces-output points at but can't reach) is enough to get real IDs,
// since the SDK generates them independently of whether export succeeds.
//
// To slot this run into a larger trace started by something upstream (a CI
// pipeline, an orchestrator, ...), set K6_TRACE_PARENT to that trace's W3C
// traceparent value before running k6:
//
//   K6_TRACE_PARENT="00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" \
//     ./build/k6-traces run --traces-output=otel=http://127.0.0.1:4318/v1/traces examples/basic.js
//
// When K6_TRACE_PARENT is set, every VU's requests become children of that
// one external span, forming a single trace across the whole run. Without
// it, each VU gets its own trace, rooted at that VU's first request.

import http from 'k6/http';
import tracing from 'k6/x/tracing';

export const options = {
  vus: 2,
  iterations: 4,
};

export default function () {
  // No setup call needed: importing k6/x/tracing is enough. Every
  // http.get/post/... call is automatically wrapped in an OTel span:
  // method, URL, status code, and k6.vu.id/k6.iteration/k6.scenario/
  // k6.group are recorded as span attributes, and a traceparent header is
  // injected so the downstream service can continue the trace.
  http.get('https://test.k6.io/');
  http.get('https://test.k6.io/contacts.php');

  // For work that isn't an HTTP request but you still want in the trace
  // (e.g. a validation step, a wait, custom timing), start a manual span.
  const span = tracing.startSpan('validate-response', { step: 'contacts-page' });
  // ... do the work here ...
  span.end('');

  // If you need to forward the current trace context to something this
  // extension doesn't intercept (e.g. a k6/net/grpc call, or just for log
  // correlation), read it directly. Empty until --traces-output is set to a
  // real backend (see note above) and the first request has been made.
  console.log(`current traceparent: ${tracing.currentTraceparent()}`);
}
