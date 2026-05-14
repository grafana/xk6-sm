// Copyright (C) 2025-2026 Grafana Labs.
// SPDX-License-Identifier: AGPL-3.0-only

package secrets

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	gsmClient "github.com/grafana/gsm-api-go-client"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

var testLogger = func() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(io.Discard)

	return l
}()

// testLimiter is an implementation of the rate.Limiter interface. It doesn't
// impose any actual rate limiting, but it allows to test the client side.
type testLimiter struct{}

func (t testLimiter) Wait(_ context.Context) error {
	return nil
}

var _ limiter = testLimiter{}

func TestGrafanaSecretsGet(t *testing.T) {
	t.Parallel()

	const (
		secretName  = "test-secret-id"
		secretValue = "test-secret-value"
		testToken   = "test-token"
	)

	setupServer := func(t *testing.T) *httptest.Server {
		t.Helper()

		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Logf("Received headers: %+v", r.Header)

			if r.Header.Get("Authorization") != "Bearer "+testToken {
				w.WriteHeader(http.StatusUnauthorized)

				return
			}

			secret := gsmClient.DecryptedSecret{
				Plaintext: secretValue,
			}

			err := json.NewEncoder(w).Encode(secret)
			if err != nil {
				t.Fatalf("failed to encode response: %v", err)
			}
		}))
	}

	setupClient := func(serverURL string) *gsmClient.Client {
		c, _ := gsmClient.NewClient(serverURL, gsmClient.WithBearerAuth(testToken))

		return c
	}

	t.Run("successful get", func(t *testing.T) {
		t.Parallel()

		server := setupServer(t)

		defer server.Close()

		grafanaSecrets := &grafanaSecrets{
			client:  setupClient(server.URL),
			limiter: testLimiter{},
			logger:  testLogger,
		}

		actual, err := grafanaSecrets.Get(secretName)
		require.NoError(t, err)
		require.Equal(t, secretValue, actual)
	})

	t.Run("with rate limit", func(t *testing.T) {
		t.Parallel()

		server := setupServer(t)

		defer server.Close()

		grafanaSecrets := &grafanaSecrets{
			client:  setupClient(server.URL),
			limiter: newLimiter(defaultRequestsPerMinuteLimit, defaultRequestsBurst),
			logger:  testLogger,
		}

		actual, err := grafanaSecrets.Get(secretName)
		require.NoError(t, err)
		require.Equal(t, secretValue, actual)
	})

	t.Run("hit rate limit", func(t *testing.T) {
		t.Parallel()

		server := setupServer(t)

		defer server.Close()

		grafanaSecrets := &grafanaSecrets{
			client:  setupClient(server.URL),
			limiter: newLimiter(120, 1),
			logger:  testLogger,
		}

		// Loop continuously for a full second, so that we exhaust the
		// rate limit. This is assuming that the test will be able to
		// make more requests than the burst limit in that time.
		timer := time.After(1 * time.Second)

		count := 0

	LOOP:
		for {
			select {
			case <-timer:
				break LOOP

			default:
				actual, err := grafanaSecrets.Get(secretName)
				require.NoError(t, err)
				require.Equal(t, secretValue, actual)

				count++
			}
		}

		// After a second, we should have hit the rate limit and then
		// some (because the bucket keeps refilling), but we cannot go
		// past it by a significant margin.
		require.GreaterOrEqual(t, count, 2)
		require.LessOrEqual(t, count, 3)

		t.Log("Total requests made:", count)
	})
}

func TestParseConfigArgument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configArg string
		wantPath  string
		wantErr   bool
	}{
		{
			name:      "valid config argument",
			configArg: "config=/path/to/config.json",
			wantPath:  "/path/to/config.json",
			wantErr:   false,
		},
		{
			name:      "empty config argument",
			configArg: "",
			wantErr:   true,
		},
		{
			name:      "no equals sign",
			configArg: "config",
			wantErr:   true,
		},
		{
			name:      "wrong key",
			configArg: "wrongkey=/path/to/config.json",
			wantErr:   true,
		},
	}

	for _, testcase := range tests {
		t.Run(testcase.name, func(t *testing.T) {
			t.Parallel()

			gotPath, err := parseConfigArgument(testcase.configArg)
			if testcase.wantErr {
				if err == nil {
					t.Errorf("parseConfigArgument() error = nil, wantErr = true")

					return
				}

				return
			}

			if err != nil {
				t.Errorf("parseConfigArgument() unexpected error = %v", err)

				return
			}

			if gotPath != testcase.wantPath {
				t.Errorf("parseConfigArgument() = %q, want %q", gotPath, testcase.wantPath)
			}
		})
	}
}

func TestGetConfig(t *testing.T) {
	t.Parallel()

	testcases := map[string]struct {
		configData   string
		expectErr    bool
		expectConfig extConfig
	}{
		"valid config": {
			configData: `{
				"url":"http://localhost",
				"token":"test-token",
				"requestsPerMinuteLimit":100,
				"requestsBurst":10
			}`,
			expectErr: false,
			expectConfig: extConfig{
				URL:                    "http://localhost",
				Token:                  "test-token",
				RequestsPerMinuteLimit: valToPtr(100),
				RequestsBurst:          valToPtr(10),
			},
		},

		"missing URL": {
			configData: `{
				"token":"test-token"
			}`,
			expectErr: true,
		},

		"missing token": {
			configData: `{
				"url":"http://localhost"
			}`,
			expectErr: true,
		},

		"invalid JSON": {
			configData: `{
				"url":"http://localhost",
				"token":"test-token",
			`, // Missing closing brace
			expectErr: true,
		},

		"empty config": {
			configData: `{}`,
			expectErr:  true,
		},

		"negative requestsPerMinuteLimit": {
			configData: `{
				"url":"http://localhost",
				"token":"test-token",
				"requestsPerMinuteLimit":-100,
				"requestsBurst":10
			}`,
			expectErr: true,
		},

		"negative requestsBurst": {
			configData: `{
				"url":"http://localhost",
				"token":"test-token",
				"requestsPerMinuteLimit":100,
				"requestsBurst":-10
			}`,
			expectErr: true,
		},

		"zero requestsBurst": {
			configData: `{
				"url":"http://localhost",
				"token":"test-token",
				"requestsPerMinuteLimit":100,
				"requestsBurst":0
			}`,
			expectErr: true,
		},

		"zero requestsPerMinuteLimit": {
			configData: `{
				"url":"http://localhost",
				"token":"test-token",
				"requestsPerMinuteLimit":0,
				"requestsBurst":10
			}`,
			expectErr: true,
		},

		"missing requestsPerMinuteLimit": {
			configData: `{
				"url":"http://localhost",
				"token":"test-token",
				"requestsBurst":10
			}`,
			expectErr: false,
			expectConfig: extConfig{
				URL:                    "http://localhost",
				Token:                  "test-token",
				RequestsPerMinuteLimit: valToPtr(defaultRequestsPerMinuteLimit),
				RequestsBurst:          valToPtr(10),
			},
		},

		"missing requestsBurst": {
			configData: `{
				"url":"http://localhost",
				"token":"test-token",
				"requestsPerMinuteLimit":100
			}`,
			expectErr: false,
			expectConfig: extConfig{
				URL:                    "http://localhost",
				Token:                  "test-token",
				RequestsPerMinuteLimit: valToPtr(100),
				RequestsBurst:          valToPtr(defaultRequestsBurst),
			},
		},
	}

	for name, testcase := range testcases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tmpFile, err := os.CreateTemp(t.TempDir(), "config-*.json")
			require.NoError(t, err)

			defer os.Remove(tmpFile.Name())

			_, err = tmpFile.WriteString(testcase.configData)
			require.NoError(t, err)
			tmpFile.Close()

			configArg := "config=" + tmpFile.Name()
			config, err := getConfig(configArg)

			if testcase.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, testcase.expectConfig, config)
			}
		})
	}
}

func TestGrafanaSecretsGetRetry(t *testing.T) {
	t.Parallel()

	const (
		secretName  = "test-secret-id"
		secretValue = "test-secret-value"
		testToken   = "test-token"
	)

	makeClient := func(serverURL string) *gsmClient.Client {
		c, _ := gsmClient.NewClient(serverURL, gsmClient.WithBearerAuth(testToken))

		return c
	}

	makeServer := func(handler http.HandlerFunc) *httptest.Server {
		return httptest.NewServer(handler)
	}

	okResponse := func(t *testing.T, w http.ResponseWriter) {
		t.Helper()
		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(gsmClient.DecryptedSecret{Plaintext: secretValue}); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}

	t.Run("retry on transient 503 then success", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32

		server := makeServer(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) <= 2 {
				w.WriteHeader(http.StatusServiceUnavailable)

				return
			}

			okResponse(t, w)
		})
		defer server.Close()

		gs := &grafanaSecrets{client: makeClient(server.URL), limiter: testLimiter{}, logger: testLogger}
		val, err := gs.Get(secretName)
		require.NoError(t, err)
		require.Equal(t, secretValue, val)
		require.EqualValues(t, 3, calls.Load())
	})

	t.Run("retry exhausted on persistent 503", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32

		server := makeServer(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		})
		defer server.Close()

		gs := &grafanaSecrets{client: makeClient(server.URL), limiter: testLimiter{}, logger: testLogger}
		_, err := gs.Get(secretName)
		require.ErrorIs(t, err, errTooManyRetries)
		require.EqualValues(t, maxAttempts, calls.Load())
	})

	t.Run("retry on 429 with Retry-After header then success", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32

		server := makeServer(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)

				return
			}

			okResponse(t, w)
		})
		defer server.Close()

		gs := &grafanaSecrets{client: makeClient(server.URL), limiter: testLimiter{}, logger: testLogger}
		val, err := gs.Get(secretName)
		require.NoError(t, err)
		require.Equal(t, secretValue, val)
		require.EqualValues(t, 2, calls.Load())
	})

	t.Run("retry on 429 without Retry-After header then success", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32

		server := makeServer(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusTooManyRequests)

				return
			}

			okResponse(t, w)
		})
		defer server.Close()

		gs := &grafanaSecrets{client: makeClient(server.URL), limiter: testLimiter{}, logger: testLogger}
		val, err := gs.Get(secretName)
		require.NoError(t, err)
		require.Equal(t, secretValue, val)
		require.EqualValues(t, 2, calls.Load())
	})

	t.Run("non-retriable 401 returns error immediately", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32

		server := makeServer(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
		})
		defer server.Close()

		gs := &grafanaSecrets{client: makeClient(server.URL), limiter: testLimiter{}, logger: testLogger}
		_, err := gs.Get(secretName)
		require.Error(t, err)
		require.ErrorIs(t, err, errFailedToGetSecret)
		require.NotErrorIs(t, err, errTooManyRetries)
		require.EqualValues(t, 1, calls.Load())
	})

	t.Run("non-retriable 404 returns error immediately", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32

		server := makeServer(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusNotFound)
		})
		defer server.Close()

		gs := &grafanaSecrets{client: makeClient(server.URL), limiter: testLimiter{}, logger: testLogger}
		_, err := gs.Get(secretName)
		require.Error(t, err)
		require.ErrorIs(t, err, errFailedToGetSecret)
		require.NotErrorIs(t, err, errTooManyRetries)
		require.EqualValues(t, 1, calls.Load())
	})

	t.Run("Retry-After exceeds budget returns errTooManyRetries", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32

		server := makeServer(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Retry-After", "999")
			w.WriteHeader(http.StatusTooManyRequests)
		})
		defer server.Close()

		gs := &grafanaSecrets{client: makeClient(server.URL), limiter: testLimiter{}, logger: testLogger}
		_, err := gs.Get(secretName)
		require.ErrorIs(t, err, errTooManyRetries)
		require.EqualValues(t, 1, calls.Load())
	})

	t.Run("network error retried then success", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32

		server := makeServer(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				hijacker, ok := w.(http.Hijacker)
				if !ok {
					t.Errorf("response writer is not a Hijacker")

					return
				}

				conn, _, err := hijacker.Hijack()
				if err != nil {
					t.Errorf("hijack failed: %v", err)

					return
				}

				_ = conn.Close()

				return
			}

			okResponse(t, w)
		})
		defer server.Close()

		gs := &grafanaSecrets{client: makeClient(server.URL), limiter: testLimiter{}, logger: testLogger}
		val, err := gs.Get(secretName)
		require.NoError(t, err)
		require.Equal(t, secretValue, val)
		require.EqualValues(t, 2, calls.Load())
	})

	t.Run("network error exhausted does not return errTooManyRetries", func(t *testing.T) {
		t.Parallel()

		// Point the client at a port that should refuse all connections.
		// The transport error returned from every attempt must take the
		// default branch in Get, not the retryError branch.
		gs := &grafanaSecrets{client: makeClient("http://127.0.0.1:1"), limiter: testLimiter{}, logger: testLogger}
		_, err := gs.Get(secretName)
		require.Error(t, err)
		require.NotErrorIs(t, err, errTooManyRetries)
	})
}

func valToPtr[T any](v T) *T { return &v }
