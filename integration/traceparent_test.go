//go:build integration

// Package integration builds a real k6 binary with the xk6-traces extension
// via xk6, runs it against a local script and HTTP server, and confirms
// requests are instrumented automatically - no tracing.instrument() call
// anywhere in the script under test.
//
// Excluded from the default `go test ./...` run by the integration build
// tag, since it requires `xk6` on PATH and a sibling k6 checkout to build
// against (see README.md's "Integration tests" section). Run explicitly via:
//
//	go test -tags=integration ./integration/... -v
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const buildTimeout = 5 * time.Minute

var k6BinaryPath string

// TestMain builds the k6-traces binary once, before any test in this
// package runs, and tears down the scratch build directory afterward.
func TestMain(m *testing.M) {
	os.Exit(runTestMain(m))
}

// runTestMain does the real work, so the temp-dir cleanup deferred within it
// actually runs - os.Exit in TestMain itself would skip any defers there.
func runTestMain(m *testing.M) int {
	tmpDir, err := os.MkdirTemp("", "xk6-traces-integration-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "creating temp dir for k6 build:", err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			fmt.Fprintln(os.Stderr, "removing temp build dir:", err)
		}
	}()

	path, err := buildK6Binary(tmpDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "building k6-traces binary:", err)
		return 1
	}
	k6BinaryPath = path

	return m.Run()
}

// buildK6Binary shells out to `xk6 build`, wiring in this checkout of
// xk6-traces and the local k6 checkout the module's go.mod is already
// replaced against (../k6, relative to the module root - here, ../../k6
// relative to this package). Both paths are resolved to absolute paths
// before being passed to xk6, since xk6 runs the actual build in its own
// scratch directory.
func buildK6Binary(outDir string) (string, error) {
	if _, err := exec.LookPath("xk6"); err != nil {
		return "", fmt.Errorf(
			"xk6 not found on PATH - install it with `go install go.k6.io/xk6/cmd/xk6@latest`: %w", err,
		)
	}

	moduleRoot, err := filepath.Abs("..")
	if err != nil {
		return "", fmt.Errorf("resolving module root: %w", err)
	}

	k6Dir, err := filepath.Abs(filepath.Join("..", "..", "k6"))
	if err != nil {
		return "", fmt.Errorf("resolving k6 checkout path: %w", err)
	}
	if info, err := os.Stat(k6Dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf(
			"expected a k6 checkout at %s (sibling of xk6-traces, matching this "+
				"module's go.mod replace directive) - none found", k6Dir,
		)
	}

	outPath := filepath.Join(outDir, "k6-traces")

	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "xk6", "build",
		"--with", "github.com/grafana/xk6-http-traces="+moduleRoot,
		"--replace", "go.k6.io/k6/v2="+k6Dir,
		"-o", outPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("xk6 build failed: %w\n%s", err, out)
	}

	return outPath, nil
}

// observedRequest is one request as seen by the app server: the headers the
// script set to identify itself, and the traceparent header the tracing
// RoundTripper should have injected.
type observedRequest struct {
	vu          string
	iter        string
	traceparent string
}

var traceparentRe = regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)

func TestAutomaticInstrumentation(t *testing.T) {
	var (
		mu       sync.Mutex
		observed []observedRequest
	)

	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tp := r.Header.Get("traceparent")

		mu.Lock()
		observed = append(observed, observedRequest{
			vu:          r.Header.Get("X-Test-VU"),
			iter:        r.Header.Get("X-Test-Iter"),
			traceparent: tp,
		})
		mu.Unlock()

		body, err := json.Marshal(map[string]string{"traceparent": tp})
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer appServer.Close()

	// Fake OTLP/HTTP collector: accepts and discards any export, and always
	// answers 200 immediately, so --traces-output=otel=... exports succeed
	// fast and don't hit the slow-shutdown-against-an-unreachable-endpoint
	// case documented in README.md.
	otelCollector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer otelCollector.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	scriptPath, err := filepath.Abs(filepath.Join("testdata", "traceparent_check.js"))
	require.NoError(t, err)

	cmd := exec.CommandContext(ctx, k6BinaryPath, "run",
		"--traces-output", "otel="+otelCollector.URL+"/v1/traces,proto=http",
		scriptPath,
	)
	cmd.Env = append(os.Environ(), "TRACING_TEST_SERVER_URL="+appServer.URL)

	out, err := cmd.CombinedOutput()
	t.Logf("k6 run output:\n%s", out)
	require.NoError(t, err, "k6 run exited non-zero")

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, observed, 4, "expected 2 VUs x 2 iterations = 4 requests")

	traceIDForVU := map[string]string{}
	for _, o := range observed {
		require.Regexp(t, traceparentRe, o.traceparent,
			"vu=%s iter=%s: malformed or missing traceparent", o.vu, o.iter)

		traceID := o.traceparent[3:35]
		if existing, ok := traceIDForVU[o.vu]; ok {
			require.Equal(t, existing, traceID,
				"vu=%s: expected every iteration of the same VU to share one root trace ID", o.vu)
		} else {
			traceIDForVU[o.vu] = traceID
		}
	}

	require.Len(t, traceIDForVU, 2, "expected one distinct trace ID per VU")
	seen := map[string]bool{}
	for vu, traceID := range traceIDForVU {
		require.False(t, seen[traceID], "vu=%s: trace ID collided with another VU's root", vu)
		seen[traceID] = true
	}
}
