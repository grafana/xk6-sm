package tracing

import (
	"context"
	"net/http"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"go.k6.io/k6/v2/js/modules"
	"go.k6.io/k6/v2/lib"
)

// spanState pairs a started span with the context carrying it, so it can
// both parent further spans (ctx) and be ended later (span).
type spanState struct {
	ctx  context.Context
	span trace.Span
}

// tracingRoundTripper wraps an http.RoundTripper, starting an OTel client
// span for every request that passes through it, injecting a W3C
// traceparent header so downstream services can continue the trace, and
// recording the response status/error on the span.
//
// It is installed once per VU as the innermost layer of state.Transport (see
// ensureWrapped in tracing.go) - lib.State.Transport is the single choke
// point all k6/http traffic passes through.
//
// Span hierarchy: a "vu" span spans this VU's entire lifetime (vuRoot,
// started here at construction time - i.e. the VU's first IterStart - and
// ended by endVURoot when the run's Exit event fires); an "iteration" span
// spans one IterStart..IterEnd pair (iter, via startIteration/endIteration,
// called from tracing.go's event handlers); and a request span like any
// other is created per HTTP request, parented under whichever iteration
// span is currently open. If K6_TRACE_PARENT is set, there is no vuRoot at
// all: every iteration span becomes a direct child of that one external
// span instead, forming a single trace for the whole run regardless of VU
// count, same as before iteration spans existed.
type tracingRoundTripper struct {
	next   http.RoundTripper
	tracer trace.Tracer
	vu     modules.VU

	parentCtx context.Context
	hasParent bool

	// vuRoot is this VU's root span, set once at construction time and
	// never reassigned; nil when hasParent, since the external parent
	// already unifies every VU into one trace.
	vuRoot *spanState

	// iter holds the currently open iteration span, if any - set by
	// startIteration at IterStart, cleared (and ended) by endIteration at
	// the matching IterEnd. nil between iterations, and before the first
	// one (e.g. during setup()/teardown(), which don't emit IterStart).
	iter atomic.Pointer[spanState]
}

func newTracingRoundTripper(
	next http.RoundTripper,
	tracer trace.Tracer,
	vu modules.VU,
	parentCtx context.Context,
	state *lib.State,
) *tracingRoundTripper {
	t := &tracingRoundTripper{
		next:      next,
		tracer:    tracer,
		vu:        vu,
		parentCtx: parentCtx,
		hasParent: trace.SpanContextFromContext(parentCtx).IsValid(),
	}

	if !t.hasParent {
		ctx, span := tracer.Start(vu.Context(), "vu", trace.WithAttributes(k6Attributes(state)...))
		t.vuRoot = &spanState{ctx: ctx, span: span}
	}

	return t
}

// RoundTrip starts a span for req, propagates it downstream via a
// traceparent header, delegates to the wrapped transport, and records the
// outcome on the span before returning.
func (t *tracingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// req.Context() descends from the VU/iteration execution context (with a
	// per-request timeout added by k6's own request pipeline).
	ctx, span := t.tracer.Start(t.parentFor(req.Context()), spanName(req), trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

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

// startIteration starts a new "iteration" span, parented under
// iterationParent(baseCtx), and stores it as the currently open iteration.
func (t *tracingRoundTripper) startIteration(baseCtx context.Context, attrs []attribute.KeyValue) {
	ctx, span := t.tracer.Start(t.iterationParent(baseCtx), "iteration", trace.WithAttributes(attrs...))
	t.iter.Store(&spanState{ctx: ctx, span: span})
}

// endIteration ends the currently open iteration span, if any, marking it as
// an error span if err is non-nil (the iteration itself failed - e.g. an
// uncaught script exception).
func (t *tracingRoundTripper) endIteration(err error) {
	v := t.iter.Swap(nil)
	if v == nil {
		return
	}
	if err != nil {
		v.span.RecordError(err)
		v.span.SetStatus(codes.Error, err.Error())
	}
	v.span.End()
}

// endVURoot ends this VU's root span, if one was created (i.e. K6_TRACE_PARENT
// wasn't set). Called once, from tracing.go's Exit handler.
func (t *tracingRoundTripper) endVURoot() {
	if t.vuRoot == nil {
		return
	}
	t.vuRoot.span.End()
}

// iterationParent returns the context an iteration span (or, if none is
// open, a request/manual span) should be parented under: the externally
// supplied parent (K6_TRACE_PARENT) if one was given, this VU's root span
// otherwise, or baseCtx unchanged if neither exists (shouldn't normally
// happen outside of setup()/teardown(), which don't emit IterStart).
func (t *tracingRoundTripper) iterationParent(baseCtx context.Context) context.Context {
	switch {
	case t.hasParent:
		return trace.ContextWithRemoteSpanContext(baseCtx, trace.SpanContextFromContext(t.parentCtx))
	case t.vuRoot != nil:
		return trace.ContextWithSpanContext(baseCtx, trace.SpanContextFromContext(t.vuRoot.ctx))
	default:
		return baseCtx
	}
}

// parentFor returns the context tracer.Start should use for req: the
// currently open iteration span, or - if none is open - the same fallback
// iterationParent uses for the iteration span itself.
func (t *tracingRoundTripper) parentFor(reqCtx context.Context) context.Context {
	if v := t.iter.Load(); v != nil {
		return trace.ContextWithSpanContext(reqCtx, trace.SpanContextFromContext(v.ctx))
	}
	return t.iterationParent(reqCtx)
}

// currentContext returns this VU's current trace context for callers
// outside of RoundTrip (StartSpan, CurrentTraceparent): the currently open
// iteration span, the externally supplied parent, this VU's root span, or a
// context with no span, in that order of preference.
func (t *tracingRoundTripper) currentContext() context.Context {
	if v := t.iter.Load(); v != nil {
		return v.ctx
	}
	if t.hasParent {
		return t.parentCtx
	}
	if t.vuRoot != nil {
		return t.vuRoot.ctx
	}
	return context.Background()
}

// spanName follows the common OTel HTTP client convention of "<method>
// <host>" rather than the full URL, to avoid unbounded span-name
// cardinality from path/query variation.
func spanName(req *http.Request) string {
	return req.Method + " " + req.URL.Host
}
