package tracing

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/grafana/sobek"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"go.k6.io/k6/v2/js/common"
	"go.k6.io/k6/v2/js/modules"
	"go.k6.io/k6/v2/lib"
	"go.k6.io/k6/v2/x/events"
)

// newModuleTestState builds on newTestState (roundtripper_test.go) with a
// real, in-memory-exporter-backed TracerProvider, needed here since
// ensureWrapped (unlike the roundtripper tests, which construct
// tracingRoundTripper directly with an explicit tracer) calls
// state.TracerProvider.Tracer(...) itself, and these tests assert on the
// spans actually produced through the full event-driven path.
func newModuleTestState(t *testing.T) (*lib.State, *tracetest.InMemoryExporter) {
	t.Helper()
	tp, exporter := newTestTracerProvider(t)
	state := newTestState()
	state.TracerProvider = tp
	return state, exporter
}

// fakeSubscriber is a minimal, self-contained implementation of
// events.Subscriber (the interface behind vu.Events().Local/Global). It lets
// tests emit events directly, without depending on k6's internal dispatcher,
// which this module can't import.
type fakeSubscriber struct {
	mu     sync.Mutex
	nextID uint64
	subs   map[uint64]chan *events.Event
}

func (f *fakeSubscriber) Subscribe(_ ...events.Type) (uint64, <-chan *events.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.nextID++
	id := f.nextID
	ch := make(chan *events.Event, 1)
	if f.subs == nil {
		f.subs = make(map[uint64]chan *events.Event)
	}
	f.subs[id] = ch
	return id, ch
}

func (f *fakeSubscriber) Unsubscribe(subID uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if ch, ok := f.subs[subID]; ok {
		close(ch)
		delete(f.subs, subID)
	}
}

// emit sends evt to every current subscriber and waits (with a test-scale
// timeout) for each to call Done, mirroring the blocking contract k6's real
// dispatcher provides via emitAndWaitEvent.
func (f *fakeSubscriber) emit(t *testing.T, evt *events.Event) {
	t.Helper()

	f.mu.Lock()
	chans := make([]chan *events.Event, 0, len(f.subs))
	for _, ch := range f.subs {
		chans = append(chans, ch)
	}
	f.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(len(chans))
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	origDone := evt.Done
	evt.Done = func() {
		if origDone != nil {
			origDone()
		}
		wg.Done()
	}

	for _, ch := range chans {
		ch <- evt
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event subscribers to call Done")
	}
}

func (f *fakeSubscriber) subscriptionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subs)
}

var _ events.Subscriber = (*fakeSubscriber)(nil)

// instrumentedFakeVU implements modules.VU with working Local/Global event
// subscribers, so tests can exercise NewModuleInstance's automatic
// instrumentation without a real k6 runtime.
type instrumentedFakeVU struct {
	state  *lib.State
	local  fakeSubscriber
	global fakeSubscriber
}

func (f *instrumentedFakeVU) Context() context.Context { return context.Background() }
func (f *instrumentedFakeVU) Events() common.Events {
	return common.Events{Local: &f.local, Global: &f.global}
}
func (f *instrumentedFakeVU) InitEnv() *common.InitEnvironment {
	return &common.InitEnvironment{
		TestPreInitState: &lib.TestPreInitState{
			LookupEnv: func(string) (string, bool) { return "", false },
			Logger:    logrus.New(),
		},
	}
}
func (f *instrumentedFakeVU) State() *lib.State       { return f.state }
func (f *instrumentedFakeVU) Runtime() *sobek.Runtime { return nil }
func (f *instrumentedFakeVU) RegisterCallback() func(func() error) {
	return func(func() error) {}
}

var _ modules.VU = (*instrumentedFakeVU)(nil)

func TestNewModuleInstance_WrapsTransportOnIterStart(t *testing.T) {
	t.Parallel()

	state, _ := newModuleTestState(t)
	vu := &instrumentedFakeVU{state: state}
	root := New()
	instance := root.NewModuleInstance(vu)
	inst, ok := instance.(*Instance)
	require.True(t, ok)

	require.Nil(t, vu.state.Transport, "transport should be untouched before IterStart")

	vu.local.emit(t, &events.Event{Type: events.IterStart, Data: events.IterData{VUID: 1}})

	require.NotNil(t, vu.state.Transport, "expected IterStart to trigger the automatic wrap")
	_, ok = vu.state.Transport.(*tracingRoundTripper)
	require.True(t, ok, "expected state.Transport to be wrapped with tracingRoundTripper")
	require.NotNil(t, inst.rt)
}

func TestNewModuleInstance_ExitUnsubscribesEventSubscriptions(t *testing.T) {
	t.Parallel()

	state, _ := newModuleTestState(t)
	vu := &instrumentedFakeVU{state: state}
	root := New()
	root.NewModuleInstance(vu)

	require.Equal(t, 1, vu.local.subscriptionCount())
	require.Equal(t, 1, vu.global.subscriptionCount())

	vu.global.emit(t, &events.Event{Type: events.Exit})

	require.Eventually(t, func() bool {
		return vu.local.subscriptionCount() == 0 && vu.global.subscriptionCount() == 0
	}, 5*time.Second, 10*time.Millisecond, "expected Exit to unsubscribe both event subscriptions")
}

func TestNewModuleInstance_ExitWithoutIterationDoesNotHang(t *testing.T) {
	t.Parallel()

	state, _ := newModuleTestState(t)
	vu := &instrumentedFakeVU{state: state}
	root := New()
	instance := root.NewModuleInstance(vu)
	inst, ok := instance.(*Instance)
	require.True(t, ok)

	vu.global.emit(t, &events.Event{Type: events.Exit})

	require.Eventually(t, func() bool {
		return vu.local.subscriptionCount() == 0 && vu.global.subscriptionCount() == 0
	}, 5*time.Second, 10*time.Millisecond)
	require.Nil(t, inst.rt, "no iteration ever ran, so nothing should have been wrapped")
}

func TestNewModuleInstance_IterationSpanLifecycle(t *testing.T) {
	t.Parallel()

	state, exporter := newModuleTestState(t)
	vu := &instrumentedFakeVU{state: state}
	root := New()
	root.NewModuleInstance(vu)

	vu.local.emit(t, &events.Event{Type: events.IterStart, Data: events.IterData{VUID: 1, Iteration: 0}})
	vu.local.emit(t, &events.Event{Type: events.IterEnd, Data: events.IterData{VUID: 1, Iteration: 0}})

	spans := exporter.GetSpans()
	require.Len(t, spans, 1, "expected exactly one exported span: the ended iteration")
	iterSpan := spans[0]
	require.Equal(t, "iteration", iterSpan.Name)

	attrs := map[string]string{}
	for _, kv := range iterSpan.Attributes {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	require.Equal(t, "1", attrs["k6.vu.id"])

	vu.global.emit(t, &events.Event{Type: events.Exit})

	spans = exporter.GetSpans()
	require.Len(t, spans, 2, "expected the VU-root span to be exported once Exit ends it")

	var vuRootSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "vu" {
			vuRootSpan = &spans[i]
		}
	}
	require.NotNil(t, vuRootSpan, "expected an exported \"vu\" root span")
	require.Equal(t, vuRootSpan.SpanContext.TraceID(), iterSpan.SpanContext.TraceID(),
		"the iteration span should share its trace ID with the VU root")
	require.Equal(t, vuRootSpan.SpanContext.SpanID(), iterSpan.Parent.SpanID(),
		"the iteration span's parent should be the VU root")
}

func TestNewModuleInstance_IterationErrorMarksSpanFailed(t *testing.T) {
	t.Parallel()

	state, exporter := newModuleTestState(t)
	vu := &instrumentedFakeVU{state: state}
	root := New()
	root.NewModuleInstance(vu)

	vu.local.emit(t, &events.Event{Type: events.IterStart, Data: events.IterData{VUID: 1}})
	vu.local.emit(t, &events.Event{Type: events.IterEnd, Data: events.IterData{VUID: 1, Error: assertAnError{}}})

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status.Code)
}

// assertAnError is a trivial error used to exercise the IterData.Error path
// without depending on testify's assert package here.
type assertAnError struct{}

func (assertAnError) Error() string { return "boom" }
