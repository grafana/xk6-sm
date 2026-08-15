package tracing

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"go.k6.io/k6/v2/js/modules"
)

// tracingRoundTripper wraps an http.RoundTripper, starting an OTel client
// span for every request that passes through it, injecting a W3C
// traceparent header so downstream services can continue the trace, and
// recording the response status/error on the span.
//
// It is installed once per VU as the innermost layer of state.Transport (see
// ensureWrapped in tracing.go) - lib.State.Transport is the single choke
// point all k6/http traffic passes through.
//
// Root span model: if K6_TRACE_PARENT is set, every request in every VU
// becomes a child of that one external span, forming a single trace for the
// whole run. Otherwise, each VU's first request becomes that VU's own root
// span, and every later request in the same VU becomes a child of it - one
// trace per VU, distinguishable via the k6.vu.id attribute. The root span is
// an ordinary request span like any other (ended normally via defer
// span.End() in its own RoundTrip call), so there's no separate span whose
// lifecycle needs managing.
type tracingRoundTripper struct {
	next   http.RoundTripper
	tracer trace.Tracer
	vu     modules.VU

	parentCtx context.Context
	hasParent bool

	// rootOnce/rootCtx implement the per-VU root-span rendezvous described
	// above. Concurrent first requests within the same VU (e.g. from
	// http.batch()) may race to become the root; sync.Once guarantees
	// exactly one winner is recorded, though a batch's concurrent opening
	// requests may occasionally each start as independent roots before the
	// winner is recorded - a known, accepted v1 limitation.
	rootOnce sync.Once
	rootCtx  atomic.Value // holds context.Context, set once by rootOnce
}

func newTracingRoundTripper(
	next http.RoundTripper,
	tracer trace.Tracer,
	vu modules.VU,
	parentCtx context.Context,
) *tracingRoundTripper {
	return &tracingRoundTripper{
		next:      next,
		tracer:    tracer,
		vu:        vu,
		parentCtx: parentCtx,
		hasParent: trace.SpanContextFromContext(parentCtx).IsValid(),
	}
}

// RoundTrip starts a span for req, propagates it downstream via a
// traceparent header, delegates to the wrapped transport, and records the
// outcome on the span before returning.
func (t *tracingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// req.Context() descends from the VU/iteration execution context (with a
	// per-request timeout added by k6's own request pipeline).
	ctx, span := t.tracer.Start(t.parentFor(req.Context()), spanName(req), trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	if !t.hasParent {
		t.rootOnce.Do(func() { t.rootCtx.Store(ctx) })
	}

	span.SetAttributes(requestAttributes(req)...)
	if state := t.vu.State(); state != nil {
		span.SetAttributes(k6Attributes(state)...)
	}

	// Propagate the span context to the downstream service via the standard
	// W3C traceparent header.
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := t.next.RoundTrip(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return resp, err
	}

	span.SetAttributes(responseAttributes(resp)...)
	if resp.StatusCode >= http.StatusBadRequest {
		span.SetStatus(codes.Error, http.StatusText(resp.StatusCode))
	}

	return resp, nil
}

// parentFor returns the context tracer.Start should use for req: the
// externally supplied parent (K6_TRACE_PARENT) if one was given, this VU's
// already-established root span if one exists yet, or reqCtx unchanged
// (letting Start create a fresh root span) otherwise.
func (t *tracingRoundTripper) parentFor(reqCtx context.Context) context.Context {
	if t.hasParent {
		return trace.ContextWithRemoteSpanContext(reqCtx, trace.SpanContextFromContext(t.parentCtx))
	}
	if root, ok := t.rootCtx.Load().(context.Context); ok {
		return trace.ContextWithSpanContext(reqCtx, trace.SpanContextFromContext(root))
	}
	return reqCtx
}

// currentContext returns this VU's current trace context for callers
// outside of RoundTrip (StartSpan, CurrentTraceparent): the externally
// supplied parent if one was given, this VU's established root span if any
// request has been made yet, or a context with no span otherwise.
func (t *tracingRoundTripper) currentContext() context.Context {
	if t.hasParent {
		return t.parentCtx
	}
	if root, ok := t.rootCtx.Load().(context.Context); ok {
		return root
	}
	return context.Background()
}

// spanName follows the common OTel HTTP client convention of "<method>
// <host>" rather than the full URL, to avoid unbounded span-name
// cardinality from path/query variation.
func spanName(req *http.Request) string {
	return req.Method + " " + req.URL.Host
}
