package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestSMK6(t *testing.T) {
	t.Parallel()

	smk6 := os.Getenv("TEST_SMK6")
	if smk6 == "" {
		smk6 = filepath.Join("..", "dist", "sm-k6-"+runtime.GOOS+"-"+runtime.GOARCH)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	const crocochromeImage = "ghcr.io/grafana/crocochrome:v0.4.1"
	cc, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Name:         "crocochrome",
			Image:        crocochromeImage,
			ExposedPorts: []string{"8080/tcp"},
			WaitingFor:   wait.ForListeningPort("8080/tcp"),
			// Since https://github.com/grafana/crocochrome/pull/12, crocochrome requires /chromium-tmp to exist
			// and be writable.
			Mounts: testcontainers.Mounts(testcontainers.VolumeMount("chromium-tmp", "/chromium-tmp")),
		},
	})
	if err != nil {
		t.Fatalf("starting crocochrome cotnainer: %v", err)
	}

	testcontainers.CleanupContainer(t, cc)

	ccEndpoint, err := cc.PortEndpoint(ctx, "8080/tcp", "http")
	if err != nil {
		t.Fatalf("getting crocochrome endpoint: %v", err)
	}

	t.Run("metrics", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			browser bool
			wanted  []string
			script  []byte
			timeout time.Duration
		}{
			{
				name:   "singleRequest",
				wanted: wantedMetricsHttp,
				script: scriptSingleRequest,
			},
			{
				name:    "simpleBrowser",
				wanted:  wantedMetricsBrowser,
				script:  scriptSimpleBrowser,
				browser: true,
				timeout: time.Minute,
			},
		} {
			tc := tc

			t.Run(tc.name, func(t *testing.T) {
				// Do not run these tests in parallel, as crocochrome handles only one concurrent run by design.
				// TODO: If parallelism is needed, start one crocochrome container per test.

				browserURL := ""
				if tc.browser {
					session, err := createSession(ccEndpoint)
					if err != nil {
						t.Fatalf("creating crocochrome session: %v", err)
					}

					browserURL = session.ChromiumVersion.WebSocketDebuggerURL

					t.Cleanup(func() {
						err := deleteSession(ccEndpoint, session.ID)
						if err != nil {
							t.Fatalf("deleting crocochrome session: %v", err)
						}
					})
				}

				if tc.timeout == 0 {
					tc.timeout = 30 * time.Second
				}

				ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
				t.Cleanup(cancel)

				outFile := filepath.Join(t.TempDir(), "metrics.txt")

				cmd := exec.CommandContext(ctx, smk6, "run", "-", "--address=", "-o=sm="+outFile)
				cmd.Stdin = bytes.NewReader(tc.script)
				cmd.Env = []string{
					"K6_BROWSER_WS_URL=" + browserURL,
				}
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("running sm-k6: %v\n%s", err, string(out))
				}

				out, err = os.ReadFile(outFile)
				if err != nil {
					t.Fatalf("reading output metrics: %v", err)
				}

				for _, wanted := range tc.wanted {
					if !bytes.Contains(out, []byte(wanted+"{")) { // Add { to force an exact match.
						t.Fatalf("Metric %q not found in output\n%s", wanted, string(out))
					}
				}
			})
		}
	})
}

// sessionInfo is a minimal struct that maps crocochrome's responses.
type sessionInfo struct {
	ID              string `json:"id"`
	ChromiumVersion struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	} `json:"chromiumVersion"`
}

// createSession calls the crocochrome API to obtain a session.
func createSession(endpoint string) (*sessionInfo, error) {
	resp, err := http.Post(endpoint+"/sessions", "application/json", nil)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got unexpected status %d", resp.StatusCode)
	}

	session := sessionInfo{}
	err = json.NewDecoder(resp.Body).Decode(&session)
	if err != nil {
		return nil, err
	}

	return &session, nil
}

// deleteSession calls the crocochrome API to delete a session.
func deleteSession(endpoint, sessionID string) error {
	req, err := http.NewRequest(http.MethodDelete, endpoint+"/sessions/"+sessionID, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("got unexpected status %d", resp.StatusCode)
	}

	return nil
}

var wantedMetricsHttp = []string{
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

var wantedMetricsBrowser = []string{
	"probe_browser_web_vital_fcp",
	"probe_browser_web_vital_cls",
	"probe_browser_web_vital_lcp",
	"probe_data_received_bytes",
}

var scriptSimpleBrowser = []byte(`
import { browser } from 'k6/browser';
import { check } from 'https://jslib.k6.io/k6-utils/1.5.0/index.js';

export const options = {
  scenarios: {
    ui: {
      executor: 'shared-iterations',
      options: {
        browser: {
          type: 'chromium',
        },
      },
    },
  },
  thresholds: {
    checks: ['rate==1.0'],
  },
};

export default async function () {
  const context = await browser.newContext();
  const page = await context.newPage();

  try {
    await page.goto('https://test.k6.io/');

    await page.locator('input[name="login"]').type("admin");
    await page.locator('input[name="password"]').type("123");

    await Promise.all([
      page.waitForNavigation(),
      page.locator('input[type="submit"]').click(),
    ]);

    await check(page.locator("h2"), {
      'header': async h2 => await h2.textContent() == "Welcome, admin!"
    });
  } finally {
    await page.close();
  }
}
`)
