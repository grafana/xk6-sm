// Integration test script for k6/x/tracing's automatic instrumentation.
//
// Deliberately contains no tracing.instrument() call anywhere - proving the
// wrap happens on its own, from the very first request, via the IterStart
// event subscription. See ../traceparent_test.go for how this is run and
// checked.

import http from 'k6/http';
import { check } from 'k6';
import tracing from 'k6/x/tracing';

export const options = {
  vus: 2,
  iterations: 4,
  thresholds: {
    checks: ['rate==1'],
  },
};

// traceID extracts the 32-hex-char trace ID out of a W3C traceparent value
// (format "version-traceid-spanid-flags").
function traceID(traceparent) {
  return traceparent.split('-')[1];
}

export default function () {
  const res = http.get(`${__ENV.TRACING_TEST_SERVER_URL}/`, {
    headers: {
      'X-Test-VU': String(__VU),
      'X-Test-Iter': String(__ITER),
    },
  });

  check(res, {
    'status is 200': (r) => r.status === 200,
  });

  const body = JSON.parse(res.body);
  check(body, {
    'server observed a traceparent header': (b) => !!b.traceparent,
  });

  // currentTraceparent() reports this VU's root span, not the per-request
  // child span injected into this specific request - so only the trace ID
  // (shared by every span in the VU's trace) is expected to match, not the
  // full traceparent value.
  check(body, {
    "currentTraceparent's trace ID matches what the server saw": (b) =>
      traceID(tracing.currentTraceparent()) === traceID(b.traceparent),
  });
}
