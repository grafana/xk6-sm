// Copyright (C) 2024 Grafana Labs.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	prometheus "github.com/prometheus/client_model/go"
)

// runCrocochrome executes `docker run` for the crocochrome image, forwarding port 8080 to the host.
// When the test finishes, the container is (hopefully) killed.
func runCrocochrome(t *testing.T) {
	t.Helper()

	const crocochromeImage = "ghcr.io/grafana/crocochrome:v0.10.2@sha256:294f0378efa0263363a394f6be193206aa4210b9c3a1e59dec2a637693428e15"
	t.Logf("Starting crocochrome %s", crocochromeImage)

	// Pull image ahead of time to avoid race condition with readiness probe.
	pullOut, err := exec.Command("docker", "pull", crocochromeImage).CombinedOutput()
	if err != nil {
		t.Fatalf("pulling crocochrome image: %v\n%s", err, pullOut)
	}

	readinessEndpoint := "http://localhost:8080/metrics"
	dockerArgs := []string{"run", "--rm", "-i", "-p", "8080:8080"}
	if os.Getenv("CI") != "" {
		hostname, err := os.Hostname()
		if err != nil {
			t.Fatalf("getting hostname for container ID: %v", err)
		}
		// Share the job container's network namespace so crocochrome is
		// reachable at localhost. Port mapping is not allowed in this mode.
		dockerArgs = []string{"run", "--rm", "-i", "--network=container:" + hostname}
	}

	dockerArgs = append(dockerArgs, crocochromeImage)

	dockerCmd := exec.Command("docker", dockerArgs...)
	dockerCmd.Stderr = os.Stderr
	err = dockerCmd.Start()
	if err != nil {
		t.Fatalf("starting crocochrome container: %v", err)
	}

	t.Cleanup(func() {
		_ = dockerCmd.Wait()
	})
	t.Cleanup(func() {
		if dockerCmd.Process == nil {
			return
		}

		_ = dockerCmd.Process.Signal(os.Interrupt)
	})

	// Wait until crocochrome is reachable.
	readinessCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		req, err := http.NewRequestWithContext(readinessCtx, http.MethodGet, readinessEndpoint, nil)
		if err != nil {
			t.Fatalf("building crocochrome health request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if resp != nil {
			resp.Body.Close()
		}

		if err == nil && resp.StatusCode == http.StatusOK {
			t.Logf("Crocochrome up and running")
			return
		}

		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Timeout starting crocochrome: %v", err)
		}

		if ps := dockerCmd.ProcessState; ps != nil {
			t.Fatalf("Crocochrome exited with code %v", ps.ExitCode())
		}

		t.Logf("Crocochrome not ready yet")
		time.Sleep(time.Second)
	}
}

// runBrowserScript wraps runScript, creating a crocochrome session before running k6 and passing the right WS url to
// it. The session is deleted when k6 returns.
func runBrowserScript(t *testing.T, scriptFileName string, env []string) []*prometheus.MetricFamily {
	t.Helper()

	endpoint := "http://localhost:8080"

	session, err := createSession(endpoint)
	if err != nil {
		t.Fatalf("creating crocochrome session: %v", err)
	}

	defer func() {
		err := deleteSession(endpoint, session.ID)
		if err != nil {
			t.Fatalf("deleting crocochrome session: %v", err)
		}
	}()

	env = append(env, "K6_BROWSER_WS_URL="+session.ChromiumVersion.WebSocketDebuggerURL)
	return runScript(t, scriptFileName, env)
}

type sessionInfo struct {
	ID              string `json:"id"`
	ChromiumVersion struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	} `json:"chromiumVersion"`
}

// createSession uses the crocochrome API to start a browser session.
func createSession(endpoint string) (*sessionInfo, error) {
	resp, err := http.Post(endpoint+"/sessions", "application/json", nil)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close() //nolint:errcheck // Skipping for brevity in test context.

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("got unexpected status %d:\n%s", resp.StatusCode, string(body))
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

	defer resp.Body.Close() //nolint:errcheck // Skipping for brevity in test context.

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("got unexpected status %d:\n%s", resp.StatusCode, string(body))
	}

	return nil
}
