package tracing

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.k6.io/k6/v2/lib"
	"go.k6.io/k6/v2/metrics"
)

func TestRequestAttributes(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "https://example.com:8443/path?token=secret", nil)

	attrs := requestAttributes(req)
	got := map[string]string{}
	for _, kv := range attrs {
		got[string(kv.Key)] = kv.Value.Emit()
	}

	assert.Equal(t, http.MethodPost, got["http.request.method"])
	assert.Equal(t, "https://example.com:8443/path?token=secret", got["url.full"])
	assert.Equal(t, "example.com", got["server.address"])
	assert.Equal(t, "8443", got["server.port"])
}

func TestRequestAttributes_NoExplicitPort(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "https://example.com/path", nil)

	attrs := requestAttributes(req)
	for _, kv := range attrs {
		require.NotEqual(t, "server.port", string(kv.Key), "server.port should be omitted when the URL has no explicit port")
	}
}

func TestResponseAttributes(t *testing.T) {
	t.Parallel()

	resp := &http.Response{StatusCode: http.StatusTeapot}

	attrs := responseAttributes(resp)
	require.Len(t, attrs, 1)
	assert.Equal(t, "http.response.status_code", string(attrs[0].Key))
	assert.Equal(t, int64(http.StatusTeapot), attrs[0].Value.AsInt64())
}

func TestK6Attributes(t *testing.T) {
	t.Parallel()

	registry := metrics.NewRegistry()
	tags := lib.NewVUStateTags(registry.RootTagSet().With("scenario", "default").With("group", "::my group"))

	state := &lib.State{
		VUID:      7,
		Iteration: 3,
		Tags:      tags,
	}

	attrs := k6Attributes(state)
	got := map[string]string{}
	for _, kv := range attrs {
		got[string(kv.Key)] = kv.Value.Emit()
	}

	assert.Equal(t, "7", got["k6.vu.id"])
	assert.Equal(t, "3", got["k6.iteration"])
	assert.Equal(t, "default", got["k6.scenario"])
	assert.Equal(t, "::my group", got["k6.group"])
}
