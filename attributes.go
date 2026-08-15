package tracing

import (
	"net/http"
	"net/url"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	"go.k6.io/k6/v2/lib"
)

// requestAttributes returns OTel HTTP client semantic-convention attributes
// (semconv v1.24.0, the first vendored version with the stable, non-"http.method"
// attribute set) describing the outgoing request.
//
// req.URL.String() is used verbatim for url.full, including any query
// string. Callers piping spans to a shared backend should be aware this can
// carry API keys, session tokens, or other sensitive data present in the
// URL - see the CRO risk review in the design plan.
func requestAttributes(req *http.Request) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(req.Method),
		semconv.URLFull(req.URL.String()),
		semconv.ServerAddress(req.URL.Hostname()),
	}
	if port := portFor(req.URL); port != 0 {
		attrs = append(attrs, semconv.ServerPort(port))
	}
	return attrs
}

// responseAttributes returns semconv attributes describing the response.
func responseAttributes(resp *http.Response) []attribute.KeyValue {
	return []attribute.KeyValue{
		semconv.HTTPResponseStatusCode(resp.StatusCode),
	}
}

// k6Attributes returns k6-specific span attributes identifying the VU,
// iteration, scenario and group a request belongs to. The scenario/group tag
// names match those already used by k6's own cloud insights collector
// (internal/output/cloud/insights/collect.go) for consistency.
func k6Attributes(state *lib.State) []attribute.KeyValue {
	tags := state.Tags.GetCurrentValues()
	scenario, _ := tags.Tags.Get("scenario")
	group, _ := tags.Tags.Get("group")

	return []attribute.KeyValue{
		attribute.Int64("k6.vu.id", int64(state.VUID)),
		attribute.Int64("k6.iteration", state.Iteration),
		attribute.String("k6.scenario", scenario),
		attribute.String("k6.group", group),
	}
}

// portFor returns the numeric port for u, or 0 if the URL has no explicit
// port (the scheme's well-known port is left to the backend to infer).
func portFor(u *url.URL) int {
	if p := u.Port(); p != "" {
		if port, err := strconv.Atoi(p); err == nil {
			return port
		}
	}
	return 0
}
