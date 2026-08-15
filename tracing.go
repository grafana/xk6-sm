// Package tracing implements the k6/x/tracing extension: OpenTelemetry
// distributed-tracing instrumentation for k6/http requests.
//
// Scripts must call tracing.instrument() once (idempotent, safe every
// iteration) before making HTTP requests they want traced - typically as the
// first line of their default exported function. See the design plan for why
// this can't be fully automatic: the mechanism that would make it so
// (subscribing to k6's internal IterStart event) requires a package under
// go.k6.io/k6/v2/internal/, which Go's compiler forbids importing from any
// module outside go.k6.io/k6/v2.
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
	return &Instance{
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
}

// Exports implements modules.Instance.
func (i *Instance) Exports() modules.Exports {
	return modules.Exports{Default: i}
}

// Instrument installs the tracing RoundTripper on the VU's HTTP transport,
// if not already installed. Idempotent and cheap to call on every
// iteration. Every http.get/post/... made after this call returns is
// instrumented automatically for the rest of the VU's lifetime.
func (i *Instance) Instrument() {
	i.ensureWrapped()
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
			// Instrument()/StartSpan()/CurrentTraceparent() from within an
			// iteration, where state is always non-nil.
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
