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
// harmless zero value, since tracingRoundTripper only calls State().
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

func newTestTracer(t *testing.T) (trace.Tracer, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp.Tracer("test"), exporter
}

func TestRoundTrip_StartsSpanAndInjectsTraceparent(t *testing.T) {
	t.Parallel()

	tracer, exporter := newTestTracer(t)
	inner := &roundTripFunc{fn: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	}}
	vu := &fakeVU{state: newTestState()}
	rt := newTracingRoundTripper(inner, tracer, vu, context.Background())

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
	rt := newTracingRoundTripper(inner, tracer, vu, context.Background())

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/foo", nil)
	require.NoError(t, err)

	_, err = rt.RoundTrip(req)
	require.ErrorIs(t, err, boom)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status.Code)
}

func TestRoundTrip_SubsequentRequestsBecomeChildrenOfVURoot(t *testing.T) {
	t.Parallel()

	tracer, exporter := newTestTracer(t)
	inner := &roundTripFunc{fn: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	}}
	vu := &fakeVU{state: newTestState()}
	rt := newTracingRoundTripper(inner, tracer, vu, context.Background())

	for i := 0; i < 2; i++ {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/foo", nil)
		require.NoError(t, err)
		_, err = rt.RoundTrip(req)
		require.NoError(t, err)
	}

	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	root, child := spans[0], spans[1]
	assert.Equal(t, root.SpanContext.TraceID(), child.SpanContext.TraceID())
	assert.Equal(t, root.SpanContext.SpanID(), child.Parent.SpanID())
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
	rt := newTracingRoundTripper(inner, tracer, vu, parentCtx)

	for i := 0; i < 2; i++ {
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
