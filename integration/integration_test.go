// Copyright (C) 2026 Grafana Labs.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

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
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func runScript(t *testing.T, scriptFileName string, env []string) []*dto.MetricFamily {
	t.Helper()

	smk6 := os.Getenv("TEST_SMK6")
	if smk6 == "" {
		smk6 = filepath.Join(
			"..", "dist", runtime.GOOS+"-"+runtime.GOARCH, "sm-k6",
		)
	}

	_, err := os.Stat(smk6) //nolint:gosec // Path comes from env or hardcoded relative path.
	if err != nil {
		t.Fatalf(
			"sm-k6 binary does not seem to exist, must be compiled before running this test: %v",
			err,
		)
	}

	script, err := os.ReadFile(scriptFileName)
	if err != nil {
		t.Fatalf("reading test script: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)

	smOutFile := filepath.Join(t.TempDir(), "metrics.txt")
	jsonOutFile := filepath.Join(t.TempDir(), "metrics.json")

	cmd := exec.CommandContext( //nolint:gosec // Path comes from env or hardcoded relative path.
		ctx, smk6, "run", "-",
		"--summary-mode=disabled", "--address=",
		"-o=sm="+smOutFile, "-o=json="+jsonOutFile,
	)
	cmd.Stdin = bytes.NewReader(script)
	cmd.Env = env

	k6out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running sm-k6: %v\n%s", errors.Join(err, ctx.Err()), string(k6out))
	}

	t.Logf("k6 output:\n%s", string(k6out))

	smMetrics, err := os.ReadFile(smOutFile)
	if err != nil {
		t.Fatalf("reading output metrics: %v", err)
	}

	jsonMetrics, err := os.ReadFile(jsonOutFile)
	if err != nil {
		t.Fatalf("reading json metrics: %v", err)
	}

	t.Logf("sm metrics:\n%s", string(smMetrics))
	t.Logf("json metrics:\n%s", string(jsonMetrics))

	metricFamilies := make([]*dto.MetricFamily, 0)
	decoder := expfmt.NewDecoder(
		bytes.NewReader(smMetrics), expfmt.NewFormat(expfmt.TypeTextPlain),
	)

	for {
		metricFamily := &dto.MetricFamily{}

		err := decoder.Decode(metricFamily)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("decoding metric: %v", err)
		}

		metricFamilies = append(metricFamilies, metricFamily)
	}

	if len(metricFamilies) == 0 {
		t.Fatalf("no metrics decoded")
	}

	return metricFamilies
}

//nolint:gocognit,gocyclo,cyclop // Table-driven test with many subtests.
func TestSMK6(t *testing.T) {
	t.Parallel()

	mfs := runScript(t, "test-script.js", nil)

	t.Run("wanted metrics are present", func(t *testing.T) {
		t.Parallel()

		wantedMetrics := []string{
			"probe_checks_total",
			"probe_data_received_bytes",
			"probe_data_sent_bytes",
			"probe_http_duration_seconds",
			"probe_http_error_code",
			"probe_http_got_expected_response",
			"probe_http_info",
			"probe_http_requests_failed",       // Original rate.
			"probe_http_requests_failed_total", // Computed counter.
			"probe_http_requests_total",
			"probe_http_ssl",
			"probe_http_status_code",
			"probe_http_total_duration_seconds",
			"probe_http_version",
			"probe_iteration_duration_seconds",
			"probe_script_duration_seconds",
			// Custom metrics:
			"probe_waiting_time", "probe_my_counter", "probe_my_gauge",
		}

		for _, wanted := range wantedMetrics {
			if !slices.ContainsFunc(mfs, func(m *dto.MetricFamily) bool {
				return m.GetName() == wanted
			}) {
				t.Fatalf("Metric %q not found in output", wanted)
			}
		}
	})

	t.Run("unwanted metrics are not present", func(t *testing.T) {
		t.Parallel()

		unwantedMetrics := []string{
			"probe_checks",
			"probe_http_reqs",       // Renamed s/req/requests.
			"probe_http_req_failed", // Renamed s/req/requests.
			"probe_data_sent",
			"probe_data_received",
			"probe_http_req_duration",
			"probe_iteration_duration",
			"probe_http_req_blocked",
			"probe_http_req_connecting",
			"probe_http_req_receiving",
			"probe_http_req_sending",
			"probe_http_req_tls_handshaking",
			"probe_http_req_waiting",
		}

		for _, wanted := range unwantedMetrics {
			if slices.ContainsFunc(mfs, func(m *dto.MetricFamily) bool {
				return m.GetName() == wanted
			}) {
				t.Fatalf("Metric %q not found in output", wanted)
			}
		}
	})

	t.Run("labels are present", func(t *testing.T) {
		t.Parallel()

		// requiredLabels maps metric names to label names that are required to be present on that metric.
		requiredLabels := map[string][]string{
			// NOTE: probe_http_info will not contain these labels if the
			// request failed, so an instance of this metric fails this test.
			// "probe_http_info":             {"tls_version", "proto"},
			"probe_http_duration_seconds": {"phase"},
			"probe_checks_total":          {"result"},
			"my_gauge":                    {"foo", "tab"},
		}

		for _, metricFamily := range mfs {
			for _, metric := range metricFamily.GetMetric() {
				required := requiredLabels[metricFamily.GetName()]
				for _, req := range required {
					if !slices.ContainsFunc(metric.GetLabel(), func(lp *dto.LabelPair) bool {
						return lp.GetName() == req
					}) {
						t.Fatalf(
							"metric %q does not contain label %q",
							metricFamily.GetName(), req,
						)
					}
				}
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
			"error":             {"probe_http_info"},
			"expected_response": {"probe_http_got_expected_response"},
			"group":             {},
			"__raw_url__":       {},
		}

		for _, metricFamily := range mfs {
			for _, metric := range metricFamily.GetMetric() {
				for _, labelPair := range metric.GetLabel() {
					allowedMetrics, isForbidden := forbiddenLabels[labelPair.GetName()]
					if !isForbidden {
						continue
					}

					if !slices.Contains(allowedMetrics, metricFamily.GetName()) {
						t.Fatalf(
							"%q should not contain label %q",
							metricFamily.GetName(), labelPair.GetName(),
						)
					}
				}
			}
		}
	})

	t.Run("metrics have expected values", func(t *testing.T) {
		t.Parallel()

		type testCase struct {
			name         string
			metricName   string // Metric name to assert.
			metricLabels map[string]string
			assertValue  func(float64) bool
		}

		quickpizza := "https://quickpizza.grafana.com"

		for _, testcase := range []testCase{
			// Some global metrics.
			{
				name:        "Script duration seconds",
				metricName:  "probe_script_duration_seconds",
				assertValue: nonZero,
			},
			{
				name:        "Sent bytes",
				metricName:  "probe_data_sent_bytes",
				assertValue: nonZero,
			},
			{
				name:        "Received bytes",
				metricName:  "probe_data_received_bytes",
				assertValue: nonZero,
			},
			// Check-related metrics.
			{
				name:         "Passed checks metric",
				metricName:   "probe_checks_total",
				metricLabels: map[string]string{"result": "pass"},
				assertValue:  equals(1),
			},
			{
				name:         "Failed checks metric",
				metricName:   "probe_checks_total",
				metricLabels: map[string]string{"result": "fail"},
				assertValue:  equals(2),
			},
			// Misc http metrics.
			{
				name:       "HTTP duration second has a duration-ish value",
				metricName: "probe_http_duration_seconds",
				metricLabels: map[string]string{
					"phase": "processing",
					"url":   quickpizza + "/login",
				},
				assertValue: nonZero,
			},
			{
				name:       "HTTP duration seconds has phase=resolve",
				metricName: "probe_http_duration_seconds",
				metricLabels: map[string]string{
					"phase": "resolve",
					"url":   quickpizza + "/login",
				},
				assertValue: anyValue,
			},
			{
				name:       "HTTP duration seconds has phase=connect",
				metricName: "probe_http_duration_seconds",
				metricLabels: map[string]string{
					"phase": "connect",
					"url":   quickpizza + "/login",
				},
				assertValue: anyValue,
			},
			{
				name:       "HTTP duration seconds has phase=tls",
				metricName: "probe_http_duration_seconds",
				metricLabels: map[string]string{
					"phase": "tls",
					"url":   quickpizza + "/login",
				},
				assertValue: anyValue,
			},
			{
				name:       "HTTP duration seconds has phase=processing",
				metricName: "probe_http_duration_seconds",
				metricLabels: map[string]string{
					"phase": "processing",
					"url":   quickpizza + "/login",
				},
				assertValue: anyValue,
			},
			{
				name:       "HTTP duration seconds has phase=transfer",
				metricName: "probe_http_duration_seconds",
				metricLabels: map[string]string{
					"phase": "transfer",
					"url":   quickpizza + "/login",
				},
				assertValue: anyValue,
			},
			{
				name:       "Error code for request that should succeed",
				metricName: "probe_http_error_code",
				metricLabels: map[string]string{
					"url": quickpizza + "/login",
				},
				assertValue: equals(0),
			},
			{
				name:       "Error code for request that should fail",
				metricName: "probe_http_error_code",
				metricLabels: map[string]string{
					"url": "http://fail.internal/failure-nxdomain",
				},
				assertValue: equals(1101),
			},
			{
				name:       "HTTP status code for a request that should succeed",
				metricName: "probe_http_status_code",
				metricLabels: map[string]string{
					"url": quickpizza + "/login",
				},
				assertValue: equals(200),
			},
			{
				name:       "Expected response for a request that should succeed",
				metricName: "probe_http_got_expected_response",
				metricLabels: map[string]string{
					"url": quickpizza + "/login",
				},
				assertValue: equals(1),
			},
			{
				name:       "Expected response for a request that should fail",
				metricName: "probe_http_got_expected_response",
				metricLabels: map[string]string{
					"url": quickpizza + "/thats-a-404",
				},
				assertValue: equals(0),
			},
			{
				name:       "Total requests for a URL accessed once",
				metricName: "probe_http_requests_total",
				metricLabels: map[string]string{
					"url": quickpizza + "/login",
				},
				assertValue: equals(1),
			},
			{
				name:       "Total requests for a URL accessed twice",
				metricName: "probe_http_requests_total",
				metricLabels: map[string]string{
					"url": quickpizza + "/thats-another-404-accessed-twice",
				},
				assertValue: equals(2),
			},
			{
				name:       "HTTP requests failed rate",
				metricName: "probe_http_requests_failed",
				metricLabels: map[string]string{
					"url": quickpizza + "/thats-another-404-accessed-twice",
				},
				assertValue: equals(1),
			},
			{
				name:       "HTTP requests failed total",
				metricName: "probe_http_requests_failed_total",
				metricLabels: map[string]string{
					"url": quickpizza + "/thats-another-404-accessed-twice",
				},
				assertValue: equals(2),
			},
			{
				name:       "HTTP version",
				metricName: "probe_http_version",
				metricLabels: map[string]string{
					"url": quickpizza + "/login",
				},
				assertValue: func(f float64) bool { return f >= 1.1 },
			},
			{
				name:       "TLS version label value",
				metricName: "probe_http_info",
				// Test for a particular URL to avoid matching a failed request, which has no TLS version.
				metricLabels: map[string]string{
					"tls_version": "1.3",
					"url":         quickpizza + "/login",
				},
				assertValue: anyValue,
			},
			{
				name:         "__raw_url__ overrides url",
				metricName:   "probe_http_requests_total",
				metricLabels: map[string]string{"url": "foobar"},
				assertValue:  equals(1),
			},
		} {
			t.Run(testcase.name, func(t *testing.T) {
				t.Parallel()

				matchedMetrics := 0

				for _, metricFamily := range mfs {
					if metricFamily.GetName() != testcase.metricName {
						// This is not the metric we are asserting on, skip it.
						continue
					}

				metric:
					for _, metric := range metricFamily.GetMetric() {
						for _, labelPair := range metric.GetLabel() {
							// Check each label of this particular metric against the test case labels.
							// If the metric has a label we're not matching for, that's okay, but it we are matching
							// it then the value should match as well.
							actual, present := testcase.metricLabels[labelPair.GetName()]
							if present && actual != labelPair.GetValue() {
								continue metric
							}
						}

						matchedMetrics++
						// Instead of check which type this metric has, and then use that one, rely on
						// GetValue() that does this check for us and return 0 if the type is not correct.
						metricValue := metric.GetGauge().GetValue() +
							metric.GetCounter().GetValue() +
							metric.GetUntyped().GetValue()

						if !testcase.assertValue(metricValue) {
							t.Fatalf(
								"Metric value for %q got unexpected value %v "+
									"(did not satisfy assert function)",
								metricFamily.GetName(), metricValue,
							)
						}
					}
				}

				if matchedMetrics == 0 {
					t.Fatalf(
						"Test case for %q with specified labels "+
							"matched no metric in extension output",
						testcase.metricName,
					)
				}
			})
		}
	})

	t.Run("metrics have required prefix", func(t *testing.T) {
		t.Parallel()

		for _, metricFamily := range mfs {
			if !strings.HasPrefix(metricFamily.GetName(), "probe_") {
				t.Fatalf("Metric %q not have the required prefix", metricFamily.GetName())
			}
		}
	})
}

//nolint:gocognit,cyclop // Table-driven test with many subtests.
func TestSMK6Browser(t *testing.T) {
	t.Parallel()

	runCrocochrome(t)

	t.Run("default settings", func(t *testing.T) {
		// Do not run this one in parallel, as crocochrome only supports one concurrent script run.
		mfs := runBrowserScript(t, "browser-script.js", nil) // Default allowlist.

		t.Run("includes expected metrics", func(t *testing.T) {
			t.Parallel()

			wanted := []string{
				"probe_browser_data_received",
				"probe_browser_data_sent",
				"probe_browser_http_req_duration",
				"probe_browser_http_req_failed",
				"probe_browser_web_vital_cls",
				"probe_browser_web_vital_fcp",
				"probe_browser_web_vital_lcp",
				"probe_browser_web_vital_ttfb",
			}

			for _, wantedName := range wanted {
				if !slices.ContainsFunc(mfs, func(metricFamily *dto.MetricFamily) bool {
					return metricFamily.GetName() == wantedName
				}) {
					t.Log(mfs)
					t.Fatalf("Missing metric %q", wantedName)
				}
			}
		})

		t.Run("only includes document browser metrics", func(t *testing.T) {
			t.Parallel()

			for _, metricFamily := range mfs {
				for _, metric := range metricFamily.GetMetric() {
					for _, labelPair := range metric.GetLabel() {
						if labelPair.GetName() == "resource_type" &&
							labelPair.GetValue() != "Document" {
							t.Fatalf(
								"metric %q should not have %s=%q",
								metricFamily.GetName(),
								labelPair.GetName(),
								labelPair.GetValue(),
							)
						}
					}
				}
			}
		})

		t.Run("number of timeseries is sane", func(t *testing.T) {
			t.Parallel()

			for _, metricFamily := range mfs {
				sane := 5
				if found := len(metricFamily.GetMetric()); found > sane {
					t.Fatalf(
						"Found suspiciously large number of timeseries (%d>%d) for metric %q",
						found, sane, metricFamily.GetName(),
					)
				}
			}
		})
	})

	t.Run("non-default allowlist", func(t *testing.T) {
		// Do not run this one in parallel, as crocochrome only supports one concurrent script run.

		// Custom allowlist, mixed case.
		mfs := runBrowserScript(
			t, "browser-script.js",
			[]string{"SM_K6_BROWSER_RESOURCE_TYPES=image,script"},
		)

		t.Run("only includes expected browser metrics", func(t *testing.T) {
			t.Parallel()

			expected := []string{"Image", "Script"}

			for _, metricFamily := range mfs {
				for _, metric := range metricFamily.GetMetric() {
					for _, labelPair := range metric.GetLabel() {
						if labelPair.GetName() == "resource_type" &&
							!slices.Contains(expected, labelPair.GetValue()) {
							t.Fatalf(
								"metric %q should not have %s=%q",
								metricFamily.GetName(),
								labelPair.GetName(),
								labelPair.GetValue(),
							)
						}
					}
				}
			}
		})
	})

	t.Run("allow all the things", func(t *testing.T) {
		// Do not run this one in parallel, as crocochrome only supports one concurrent script run.
		mfs := runBrowserScript(
			t, "browser-script.js",
			[]string{"SM_K6_BROWSER_RESOURCE_TYPES=*"},
		)

		t.Run("only includes expected browser metrics", func(t *testing.T) {
			t.Parallel()

			for _, metricFamily := range mfs {
				for _, metric := range metricFamily.GetMetric() {
					for _, labelPair := range metric.GetLabel() {
						if labelPair.GetName() == "resource_type" &&
							labelPair.GetValue() == "Script" {
							// We found a metric with resource_type=Script, which is not in the default allowlist.
							// Approximating this as the wildcard working, and calling the test good.
							return
						}
					}
				}
			}

			t.Fatalf("Did not found any metric with resource_type=Script")
		})
	})
}

func equals(expected float64) func(float64) bool {
	return func(v float64) bool {
		return v == expected
	}
}

func nonZero(v float64) bool {
	return v > 0
}

func anyValue(float64) bool {
	return true
}
