package integration_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSMK6(t *testing.T) {
	t.Parallel()

	smk6 := os.Getenv("TEST_SMK6")
	if smk6 == "" {
		smk6 = filepath.Join("..", "dist", "sm-k6-"+runtime.GOOS+"-"+runtime.GOARCH)
	}

	t.Run("metrics", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			script []byte
		}{
			{
				name:   "singleRequest",
				script: scriptSingleRequest,
			},
		} {
			tc := tc

			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				t.Cleanup(cancel)

				outFile := filepath.Join(t.TempDir(), "metrics.txt")

				cmd := exec.CommandContext(ctx, smk6, "run", "-", "-o=sm="+outFile)
				cmd.Stdin = bytes.NewReader(tc.script)
				err := cmd.Run()
				if err != nil {
					t.Fatalf("running sm-k6: %v", err)
				}

				out, err := os.ReadFile(outFile)
				if err != nil {
					t.Fatalf("reading output metrics: %v", err)
				}

				for _, wanted := range wantedMetrics {
					if !bytes.Contains(out, []byte(wanted+"{")) { // Add { to force an exact match.
						t.Fatalf("Metric %q not found in output", wanted)
					}
				}
			})
		}
	})
}

var wantedMetrics = []string{
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

var scriptSingleRequest = []byte(`
import http from 'k6/http';

export const options = {
  iterations: 1,
};

export default function () {
  const response = http.get('https://test-api.k6.io/public/crocodiles/');
}`)
