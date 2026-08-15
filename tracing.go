// Package tracing implements the k6/x/tracing extension: OpenTelemetry
// distributed-tracing instrumentation for k6/http requests.
//
// Instrumentation is fully automatic: importing the module is enough. Each
// VU subscribes to k6's per-iteration IterStart event
// (go.k6.io/k6/v2/event) and installs the tracing RoundTripper on
// state.Transport before that event's wait completes, which k6 guarantees
// happens before the very first line of the VU's iteration function runs -
// see ensureWrapped and watchEvents below. No script-side setup call is
// required.
//
// Export is entirely k6's own OTel pipeline: this extension only ever calls
// state.TracerProvider.Tracer(...), which is wired to a real OTLP exporter
// when the k6 run is started with --traces-output=otel=..., or a safe no-op
// otherwise. There is no exporter configuration here.
package tracing

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"go.k6.io/k6/v2/event"
	"go.k6.io/k6/v2/js/modules"
)

func init() {
	modules.Register("k6/x/tracing", New())
}

// RootModule is the global entry point registered with k6; it constructs a
// fresh Instance for each VU that imports this module.
type RootModule struct{}

// New returns a new RootModule.
func New() *RootModule {
	return &RootModule{}
}

// Instance is the per-VU state for this module.
type Instance struct {
	vu modules.VU

	// parentCtx is resolved once, from K6_TRACE_PARENT, at instance-creation
	// (init-context) time - see resolveParentContext in parent.go.
	parentCtx context.Context

	wrapOnce sync.Once

	// rt is the tracingRoundTripper installed on state.Transport by
	// ensureWrapped. It tracks this VU's current trace context (external
	// parent, or the VU's own root span once one exists), which StartSpan
	// and CurrentTraceparent need too - nil until ensureWrapped succeeds.
	rt *tracingRoundTripper
}

var (
	_ modules.Module   = (*RootModule)(nil)
	_ modules.Instance = (*Instance)(nil)
)

// NewModuleInstance implements modules.Module; called once per VU that
// imports k6/x/tracing.
func (r *RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	i := &Instance{
		vu: vu,
		parentCtx: resolveParentContext(
			context.Background(),
			vu.InitEnv().LookupEnv,
			func(value string) {
				vu.InitEnv().Logger.Warnf(
					"k6/x/tracing: ignoring malformed %s value %q", traceParentEnvVar, value,
				)
			},
		),
	}
	i.watchEvents()
	return i
}

// Exports implements modules.Instance.
func (i *Instance) Exports() modules.Exports {
	return modules.Exports{Default: i}
}

// watchEvents subscribes to this VU's IterStart event and the run's Exit
// event, and installs the handlers that drive automatic instrumentation and
// eventual cleanup. Called once per VU, from NewModuleInstance.
func (i *Instance) watchEvents() {
	exitSubID, exitCh := i.vu.Events().Global.Subscribe(event.Exit)
	iterSubID, iterCh := i.vu.Events().Local.Subscribe(event.IterStart)

	unsubscribe := func() {
		i.vu.Events().Local.Unsubscribe(iterSubID)
		i.vu.Events().Global.Unsubscribe(exitSubID)
	}

	go i.handleIterStart(iterCh)
	go i.handleExit(exitCh, unsubscribe)
}

// handleIterStart ensures state.Transport is wrapped on every IterStart
// event. k6 blocks the VU's iteration on this event's Done being called
// before running the script's exported function, so by the time this
// handler's ensureWrapped/Done pair returns, the wrap is guaranteed in place
// before any script-issued HTTP request. ensureWrapped's sync.Once makes the
// actual wrap happen only once per VU; every event after that is a cheap
// no-op.
func (i *Instance) handleIterStart(iterCh <-chan *event.Event) {
	for evt := range iterCh {
		i.ensureWrapped()
		evt.Done()
	}
}

// handleExit unsubscribes this VU's event subscriptions once the run's Exit
// event fires, so the goroutines spawned by watchEvents terminate even for
// VUs that never ran an iteration. unsubscribe is deferred ahead of Done so
// it completes before Done is signalled, matching the ordering k6 core's own
// k6/browser module uses for the same reason: it prevents a concurrent Exit
// emission from being delivered to an already-exited goroutine, which would
// leave Done uncalled and the emitter's wait blocked.
func (i *Instance) handleExit(exitCh <-chan *event.Event, unsubscribe func()) {
	var evt *event.Event
	defer func() {
		if evt != nil {
			evt.Done()
		}
	}()
	defer unsubscribe()

	received, ok := <-exitCh
	if !ok {
		return
	}
	evt = received
}

// ensureWrapped performs the actual, one-time-per-VU wrap of
// state.Transport. It's a no-op (and safely retriable) if called while
// vu.State() is still nil, i.e. from the init context.
func (i *Instance) ensureWrapped() {
	i.wrapOnce.Do(func() {
		state := i.vu.State()
		if state == nil {
			// Still in init context; nothing to wrap yet. wrapOnce means this
			// won't be retried automatically - callers are expected to invoke
			// StartSpan()/CurrentTraceparent() from within an iteration, where
			// state is always non-nil, or rely on the automatic IterStart-driven
			// wrap in watchEvents.
			return
		}

		rt := newTracingRoundTripper(
			state.Transport,
			state.TracerProvider.Tracer("go.k6.io/xk6-traces"),
			i.vu,
			i.parentCtx,
		)
		state.Transport = rt
		i.rt = rt
	})
}

// StartSpan starts a manual span for instrumenting non-HTTP work within an
// iteration, as a child of this VU's current trace context (the externally
// supplied parent, or this VU's own root span once one exists - see
// tracingRoundTripper.currentContext). It returns nil (a no-op) if called
// from the init context.
func (i *Instance) StartSpan(name string, attrs map[string]string) *jsSpan {
	i.ensureWrapped()

	state := i.vu.State()
	if state == nil || i.rt == nil {
		return nil
	}

	kvs := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		kvs = append(kvs, attribute.String(k, v))
	}

	tracer := state.TracerProvider.Tracer("go.k6.io/xk6-traces")
	_, span := tracer.Start(i.rt.currentContext(), name)
	span.SetAttributes(kvs...)

	return &jsSpan{span: span}
}

// CurrentTraceparent returns the W3C traceparent value for this VU's
// current trace context: the externally supplied parent if K6_TRACE_PARENT
// was set, this VU's own root span once its first request has been made, or
// an empty string before that (there's no trace yet). Useful for scripts
// that need to manually forward trace context to something this extension
// doesn't intercept (e.g. a k6/net/grpc call, or log correlation).
func (i *Instance) CurrentTraceparent() string {
	i.ensureWrapped()

	if i.rt == nil {
		return ""
	}

	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(i.rt.currentContext(), carrier)
	return carrier.Get("traceparent")
}

// jsSpan is a thin, script-facing wrapper around an OTel span for manual
// instrumentation via StartSpan.
type jsSpan struct {
	span trace.Span
}

// SetAttribute sets a single string attribute on the span.
func (s *jsSpan) SetAttribute(key, value string) {
	if s == nil {
		return
	}
	s.span.SetAttributes(attribute.String(key, value))
}

// End marks the span as finished. Optionally pass an error message to
// record the span as failed.
func (s *jsSpan) End(errMsg string) {
	if s == nil {
		return
	}
	if errMsg != "" {
		s.span.SetStatus(codes.Error, errMsg)
		s.span.RecordError(fmt.Errorf("%s", errMsg))
	}
	s.span.End()
}
