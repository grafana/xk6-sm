package tracing

import (
	"context"
	"net/http"
	"testing"

	"github.com/grafana/sobek"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"go.k6.io/k6/v2/js/common"
	"go.k6.io/k6/v2/js/modules"
	"go.k6.io/k6/v2/lib"
	"go.k6.io/k6/v2/metrics"
)

// roundTripFunc adapts a function to http.RoundTripper, capturing the
// request it was called with.
type roundTripFunc struct {
	fn      func(*http.Request) (*http.Response, error)
	lastReq *http.Request
}

func (f *roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	f.lastReq = req
	return f.fn(req)
}

// fakeVU implements modules.VU with just enough behavior for these tests:
// State() returns whatever *lib.State was configured; everything else is a
// harmless zero value, since tracingRoundTripper only calls State()/Context().
type fakeVU struct {
	state *lib.State
}

func (f *fakeVU) Context() context.Context         { return context.Background() }
func (f *fakeVU) Events() common.Events            { return common.Events{} }
func (f *fakeVU) InitEnv() *common.InitEnvironment { return nil }
func (f *fakeVU) State() *lib.State                { return f.state }
func (f *fakeVU) Runtime() *sobek.Runtime          { return nil }
func (f *fakeVU) RegisterCallback() func(func() error) {
	return func(func() error) {}
}

var _ modules.VU = (*fakeVU)(nil)

func newTestState() *lib.State {
	registry := metrics.NewRegistry()
	tags := lib.NewVUStateTags(registry.RootTagSet().With("scenario", "default"))
	return &lib.State{VUID: 1, Iteration: 0, Tags: tags}
}

// newTestTracerProvider returns a real SDK TracerProvider backed by an
// in-memory exporter, so tests can assert on the spans it actually produces.
// Needed (rather than just a trace.Tracer) anywhere a *lib.State.TracerProvider
// value itself is required, e.g. as an input to newTracingRoundTripper.
func newTestTracerProvider(t *testing.T) (*sdktrace.TracerProvider, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp, exporter
}

func newTestTracer(t *testing.T) (trace.Tracer, *tracetest.InMemoryExporter) {
	t.Helper()
	tp, exporter := newTestTracerProvider(t)
	return tp.Tracer("test"), exporter
}

func TestRoundTrip_StartsSpanAndInjectsTraceparent(t *testing.T) {
	t.Parallel()

	tracer, exporter := newTestTracer(t)
	inner := &roundTripFunc{fn: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	}}
	vu := &fakeVU{state: newTestState()}
	rt := newTracingRoundTripper(inner, tracer, vu, context.Background(), vu.state)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/foo", nil)
	require.NoError(t, err)

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.NotEmpty(t, inner.lastReq.Header.Get("traceparent"), "expected a traceparent header to be injected")

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "GET example.com", spans[0].Name)

	attrs := map[string]string{}
	for _, kv := range spans[0].Attributes {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	assert.Equal(t, "GET", attrs["http.request.method"])
	assert.Equal(t, "1", attrs["k6.vu.id"])
}

func TestRoundTrip_RecordsErrorStatus(t *testing.T) {
	t.Parallel()

	tracer, exporter := newTestTracer(t)
	boom := assert.AnError
	inner := &roundTripFunc{fn: func(*http.Request) (*http.Response, error) {
		return nil, boom
	}}
	vu := &fakeVU{state: newTestState()}
	rt := newTracingRoundTripper(inner, tracer, vu, context.Background(), vu.state)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/foo", nil)
	require.NoError(t, err)

	_, err = rt.RoundTrip(req)
	require.ErrorIs(t, err, boom)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status.Code)
}

func TestRoundTrip_RequestsWithoutIterationShareVURootParent(t *testing.T) {
	t.Parallel()

	tracer, exporter := newTestTracer(t)
	inner := &roundTripFunc{fn: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	}}
	vu := &fakeVU{state: newTestState()}
	rt := newTracingRoundTripper(inner, tracer, vu, context.Background(), vu.state)

	for range 2 {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/foo", nil)
		require.NoError(t, err)
		_, err = rt.RoundTrip(req)
		require.NoError(t, err)
	}

	// No iteration span was ever opened (e.g. as would happen for HTTP calls
	// made from setup()/teardown(), which don't emit IterStart) - both
	// requests fall back to the VU root directly, as siblings under it.
	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	first, second := spans[0], spans[1]
	assert.Equal(t, first.SpanContext.TraceID(), second.SpanContext.TraceID())
	assert.Equal(t, first.Parent.SpanID(), second.Parent.SpanID(),
		"expected both requests to share the same (unexported) VU-root parent")
}

func TestRoundTrip_ExternalParentIsUsedForEveryRequest(t *testing.T) {
	t.Parallel()

	tracer, exporter := newTestTracer(t)
	inner := &roundTripFunc{fn: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	}}
	vu := &fakeVU{state: newTestState()}

	parentCtx := resolveParentContext(
		context.Background(),
		func(string) (string, bool) {
			return "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", true
		},
		nil,
	)
	rt := newTracingRoundTripper(inner, tracer, vu, parentCtx, vu.state)
	require.Nil(t, rt.vuRoot, "no VU-root span should be created when an external parent is set")

	for range 2 {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/foo", nil)
		require.NoError(t, err)
		_, err = rt.RoundTrip(req)
		require.NoError(t, err)
	}

	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	wantTraceID := trace.SpanContextFromContext(parentCtx).TraceID()
	for _, s := range spans {
		assert.Equal(t, wantTraceID, s.SpanContext.TraceID())
	}
}

func TestRoundTrip_RequestsWithinIterationAreChildrenOfIterationSpan(t *testing.T) {
	t.Parallel()

	tracer, exporter := newTestTracer(t)
	inner := &roundTripFunc{fn: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	}}
	vu := &fakeVU{state: newTestState()}
	rt := newTracingRoundTripper(inner, tracer, vu, context.Background(), vu.state)

	rt.startIteration(context.Background(), k6Attributes(vu.state))
	for range 2 {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/foo", nil)
		require.NoError(t, err)
		_, err = rt.RoundTrip(req)
		require.NoError(t, err)
	}
	rt.endIteration(nil)

	spans := exporter.GetSpans()
	require.Len(t, spans, 3, "2 requests + 1 iteration span")

	var (
		iterSpan     tracetest.SpanStub
		foundIter    bool
		requestSpans []tracetest.SpanStub
	)
	for _, s := range spans {
		if s.Name == "iteration" {
			iterSpan, foundIter = s, true
		} else {
			requestSpans = append(requestSpans, s)
		}
	}
	require.True(t, foundIter, "expected an exported \"iteration\" span")
	require.Len(t, requestSpans, 2)

	for _, s := range requestSpans {
		assert.Equal(t, iterSpan.SpanContext.TraceID(), s.SpanContext.TraceID())
		assert.Equal(t, iterSpan.SpanContext.SpanID(), s.Parent.SpanID())
	}
}

func TestRoundTrip_TwoIterationsShareTraceIDWithDistinctSpans(t *testing.T) {
	t.Parallel()

	tracer, exporter := newTestTracer(t)
	inner := &roundTripFunc{fn: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	}}
	vu := &fakeVU{state: newTestState()}
	rt := newTracingRoundTripper(inner, tracer, vu, context.Background(), vu.state)

	doIteration := func() {
		rt.startIteration(context.Background(), k6Attributes(vu.state))
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/foo", nil)
		require.NoError(t, err)
		_, err = rt.RoundTrip(req)
		require.NoError(t, err)
		rt.endIteration(nil)
	}
	doIteration()
	doIteration()

	var iterSpans []tracetest.SpanStub
	for _, s := range exporter.GetSpans() {
		if s.Name == "iteration" {
			iterSpans = append(iterSpans, s)
		}
	}
	require.Len(t, iterSpans, 2)

	assert.Equal(t, iterSpans[0].SpanContext.TraceID(), iterSpans[1].SpanContext.TraceID(),
		"both iterations of the same VU should share one trace ID via the VU root")
	assert.NotEqual(t, iterSpans[0].SpanContext.SpanID(), iterSpans[1].SpanContext.SpanID(),
		"each iteration should get its own distinct span")
}

func TestRoundTrip_EndIterationRecordsErrorStatus(t *testing.T) {
	t.Parallel()

	tracer, exporter := newTestTracer(t)
	vu := &fakeVU{state: newTestState()}
	rt := newTracingRoundTripper(&roundTripFunc{}, tracer, vu, context.Background(), vu.state)

	rt.startIteration(context.Background(), k6Attributes(vu.state))
	rt.endIteration(assert.AnError)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "iteration", spans[0].Name)
	assert.Equal(t, codes.Error, spans[0].Status.Code)
}
