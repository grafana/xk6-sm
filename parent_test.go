package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestResolveParentContext(t *testing.T) {
	t.Parallel()

	const valid = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	tests := []struct {
		name      string
		env       map[string]string
		wantValid bool
		wantWarn  bool
	}{
		{
			name:      "absent",
			env:       map[string]string{},
			wantValid: false,
		},
		{
			name:      "empty",
			env:       map[string]string{traceParentEnvVar: ""},
			wantValid: false,
		},
		{
			name:      "valid traceparent",
			env:       map[string]string{traceParentEnvVar: valid},
			wantValid: true,
		},
		{
			name:      "malformed value falls back to no parent",
			env:       map[string]string{traceParentEnvVar: "not-a-traceparent"},
			wantValid: false,
			wantWarn:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var warned string
			ctx := resolveParentContext(context.Background(), func(key string) (string, bool) {
				v, ok := tt.env[key]
				return v, ok
			}, func(value string) { warned = value })

			gotValid := trace.SpanContextFromContext(ctx).IsValid()
			require.Equal(t, tt.wantValid, gotValid)

			if tt.wantWarn {
				require.NotEmpty(t, warned)
			} else {
				require.Empty(t, warned)
			}
		})
	}
}
