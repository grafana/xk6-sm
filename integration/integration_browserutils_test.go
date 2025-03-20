package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	const crocochromeImage = "ghcr.io/grafana/crocochrome:v0.5.2@sha256:64d0ae18f4323a2973fe8030a39887c966e6a99852e08c9df0d403215ef86a5e"
	t.Logf("Starting crocochrome %s", crocochromeImage)
	dockerCmd := exec.Command("docker", "run", "--rm", "-i", "-p", "8080:8080", crocochromeImage)
	dockerCmd.Stderr = os.Stderr
	err := dockerCmd.Start()
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
		req, err := http.NewRequestWithContext(readinessCtx, http.MethodGet, "http://localhost:8080/metrics", nil)
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
