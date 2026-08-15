package tracing

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// traceParentEnvVar is the environment variable holding a W3C traceparent
// value (https://www.w3.org/TR/trace-context/#traceparent-header) for an
// externally supplied parent span. When set, every VU's spans become
// children of this one external context, forming a single trace across the
// whole k6 run regardless of VU count.
const traceParentEnvVar = "K6_TRACE_PARENT"

// resolveParentContext looks up traceParentEnvVar via lookupEnv and, if
// present and well-formed, returns a context carrying the extracted remote
// span context as the parent for the current VU's spans. If the variable is
// absent or malformed, it returns ctx unchanged (each VU's own first request
// becomes its own root span).
func resolveParentContext(
	ctx context.Context,
	lookupEnv func(key string) (string, bool),
	onInvalid func(value string),
) context.Context {
	val, ok := lookupEnv(traceParentEnvVar)
	if !ok || val == "" {
		return ctx
	}

	carrier := propagation.MapCarrier{"traceparent": val}
	extracted := propagation.TraceContext{}.Extract(ctx, carrier)

	if !trace.SpanContextFromContext(extracted).IsValid() {
		if onInvalid != nil {
			onInvalid(val)
		}
		return ctx
	}

	return extracted
}
