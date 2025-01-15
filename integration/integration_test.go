package integration_test

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	prometheus "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

//go:embed test-script.js
var testScript []byte

func TestSMK6(t *testing.T) {
	t.Parallel()

	smk6 := os.Getenv("TEST_SMK6")
	if smk6 == "" {
		smk6 = filepath.Join("..", "dist", "sm-k6-"+runtime.GOOS+"-"+runtime.GOARCH)
	}

	_, err := os.Stat(smk6)
	if err != nil {
		t.Fatalf("sm-k6 binary does not seem to exist, must be compiled before running this test: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	outFile := filepath.Join(t.TempDir(), "metrics.txt")

	cmd := exec.CommandContext(ctx, smk6, "run", "-", "-o=sm="+outFile)
	cmd.Stdin = bytes.NewReader(testScript)
	err = cmd.Run()
	if err != nil {
		t.Fatalf("running sm-k6: %v", err)
	}

	out, err := os.Open(outFile)
	if err != nil {
		t.Fatalf("reading output metrics: %v", err)
	}

	mfs := []*prometheus.MetricFamily{}
	decoder := expfmt.NewDecoder(out, expfmt.FmtText)
	for {
		mf := &prometheus.MetricFamily{}
		err := decoder.Decode(mf)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("decoding metric: %v", err)
		}

		mfs = append(mfs, mf)

	}
	if len(mfs) == 0 {
		t.Fatalf("no metrics decoded")
	}

	t.Run("wanted metrics are present", func(t *testing.T) {
		t.Parallel()

		wantedMetrics := []string{
			"probe_data_received_bytes",
			"probe_data_sent_bytes",
			"probe_http_duration_seconds",
			"probe_http_error_code",
			"probe_http_got_expected_response",
			"probe_http_info",
			"probe_http_requests_failed_total",
			"probe_http_requests_total",
			"probe_http_ssl",
			"probe_http_status_code",
			"probe_http_total_duration_seconds",
			"probe_http_version",
			"probe_iteration_duration_seconds",
			"probe_script_duration_seconds",
		}

		for _, wanted := range wantedMetrics {
			if !slices.ContainsFunc(mfs, func(m *prometheus.MetricFamily) bool { return *m.Name == wanted }) {
				t.Fatalf("Metric %q not found in output", wanted)
			}
		}
	})

	t.Run("labels are not present", func(t *testing.T) {
		t.Parallel()

		// The keys of this map are the set of labels that are not allowed to be present in most metrics.
		// The values represent the list of metrics that are _allowed_ to have this label, as an exception.
		// If a metric contains a label (key), and that metric is not in the list (value), the test fails.
		type exceptForMetrics []string
		forbiddenLabels := map[string]exceptForMetrics{
			"tls_version": {"probe_http_info"},
			"proto":       {"probe_http_info"},
		}

		for _, mf := range mfs {
			for _, m := range mf.Metric {
				for _, labelPair := range m.Label {
					allowedMetrics, isForbidden := forbiddenLabels[*labelPair.Name]
					if !isForbidden {
						continue
					}

					if !slices.Contains(allowedMetrics, *mf.Name) {
						t.Fatalf("%q should not contain label %q", *mf.Name, *labelPair.Name)
					}
				}
			}
		}
	})
}
