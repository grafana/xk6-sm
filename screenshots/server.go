package screenshots

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// ChunkSize is the size of each chunk in bytes
	ChunkSize = 16 * 1024 // 50kb
)

// Server handles screenshot uploads and logging
type Server struct {
	server     *http.Server
	logger     logrus.FieldLogger
	seenHashes map[string]bool
}

// New creates a new screenshot server
func New(logger logrus.FieldLogger) *Server {

	return &Server{
		logger:     logger,
		seenHashes: make(map[string]bool),
	}
}

// parsePresignedURLEnvVar parses the K6_BROWSER_SCREENSHOTS_OUTPUT environment variable
// and returns the host and port to use for the server.
func parsePresignedURLEnvVar() (string, string, error) {
	outputConfig := os.Getenv("K6_BROWSER_SCREENSHOTS_OUTPUT")
	if outputConfig == "" {
		return "", "", fmt.Errorf("K6_BROWSER_SCREENSHOTS_OUTPUT environment variable not set")
	}

	// Parse the configuration string
	config := make(map[string]string)
	for _, part := range strings.Split(outputConfig, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return "", "", fmt.Errorf("invalid K6_BROWSER_SCREENSHOTS_OUTPUT format: %s", part)
		}
		config[kv[0]] = kv[1]
	}

	// Get the URL and parse it
	urlStr, ok := config["url"]
	if !ok {
		return "", "", fmt.Errorf("url not found in K6_BROWSER_SCREENSHOTS_OUTPUT")
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL in K6_BROWSER_SCREENSHOTS_OUTPUT: %w", err)
	}

	host := parsedURL.Hostname()
	port := parsedURL.Port()
	if port == "" {
		return "", "", fmt.Errorf("no port found in K6_BROWSER_SCREENSHOTS_OUTPUT")
	}

	return host, port, nil
}

// Start starts the HTTP server
func (s *Server) Start() error {
	host, port, err := parsePresignedURLEnvVar()
	if err != nil {
		return err
	}

	// Create the mux and register handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/screenshots/", s.handleScreenshot)
	mux.HandleFunc("/", s.handlePresignedURL)

	s.server = &http.Server{
		Addr:    net.JoinHostPort(host, port),
		Handler: mux,
	}

	// Start server in a goroutine
	errCh := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.WithError(err).Error("Screenshot server failed to start")
			errCh <- err
		}
	}()

	// Wait for server to start or fail
	select {
	case err := <-errCh:
		return fmt.Errorf("screenshot server failed to start: %w", err)
	case <-time.After(100 * time.Millisecond):
		// Server started successfully
		return nil
	}
}

func (s *Server) handlePresignedURL(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the request body
	var requestBody struct {
		Service   string `json:"service"`
		Operation string `json:"operation"`
		Files     []struct {
			Name string `json:"name"`
		} `json:"files"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		s.logger.WithError(err).Error("Failed to parse request body")
		http.Error(w, "Failed to parse request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	service := requestBody.Service

	// Convert the Files slice to a string slice for compatibility with existing code
	files := make([]string, 0, len(requestBody.Files))
	for _, file := range requestBody.Files {
		files = append(files, file.Name)
	}

	// Create URLs array for all files
	urls := make([]struct {
		Name         string `json:"name"`
		PresignedURL string `json:"pre_signed_url"`
		Method       string `json:"method"`
	}, 0, len(files))

	host, port, err := net.SplitHostPort(s.server.Addr)
	if err != nil {
		s.logger.WithError(err).Error("Failed to parse server address")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	for _, fileName := range files {
		u := &url.URL{
			Scheme: "http",
			Host:   net.JoinHostPort(host, port),
			Path:   "/screenshots/" + fileName,
		}

		urls = append(urls, struct {
			Name         string `json:"name"`
			PresignedURL string `json:"pre_signed_url"`
			Method       string `json:"method"`
		}{
			Name:         fileName,
			PresignedURL: u.String(),
			Method:       "POST",
		})
	}

	response := struct {
		Service string `json:"service"`
		URLs    []struct {
			Name         string `json:"name"`
			PresignedURL string `json:"pre_signed_url"`
			Method       string `json:"method"`
		} `json:"urls"`
	}{
		Service: service,
		URLs:    urls,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract filename from the URL path
	urlPath := r.URL.Path
	prefix := "/screenshots/"
	if !strings.HasPrefix(urlPath, prefix) {
		s.logger.Error("Invalid URL path format")
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	filename := strings.TrimPrefix(urlPath, prefix)

	// Parse multipart form with a larger size limit
	err := r.ParseMultipartForm(32 << 20) // 32 MB max
	if err != nil {
		s.logger.WithError(err).Error("Failed to parse multipart form")
		http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
		return
	}

	// Get the file from the form
	file, header, err := r.FormFile("file")
	if err != nil {
		s.logger.WithError(err).Error("Failed to get file from form")
		http.Error(w, "Failed to get file from form", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Log the file details for debugging
	s.logger.WithFields(logrus.Fields{
		"filename": header.Filename,
		"size":     header.Size,
		"type":     header.Header.Get("Content-Type"),
	}).Debug("Received file")

	// Read the file data
	data, err := io.ReadAll(file)
	if err != nil {
		s.logger.WithError(err).Error("Failed to read file data")
		http.Error(w, "Failed to read file data", http.StatusBadRequest)
		return
	}

	// Log the data length for debugging
	s.logger.WithField("data_length", len(data)).Debug("Read file data")

	// Split into chunks
	chunks := splitIntoChunks(data, ChunkSize)
	totalChunks := len(chunks)

	// Log chunk details for debugging
	s.logger.WithFields(logrus.Fields{
		"total_chunks": totalChunks,
		"chunk_size":   ChunkSize,
	}).Debug("Split data into chunks")

	// Base64 encode each chunk
	encodedChunks := make([]string, totalChunks)
	for i, chunk := range chunks {
		encodedChunks[i] = base64.StdEncoding.EncodeToString(chunk)
		s.logger.WithFields(logrus.Fields{
			"chunk_index":  i + 1,
			"chunk_size":   len(chunk),
			"encoded_size": len(encodedChunks[i]),
		}).Debug("Encoded chunk")
	}

	// Calculate SHA of the raw data
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	if s.seenHashes[hashStr] {
		s.logger.WithField("hash", hashStr).Info("Skipping duplicate screenshot")
		w.WriteHeader(http.StatusOK)
		return
	}

	s.seenHashes[hashStr] = true

	// Log each chunk
	for i, encodedChunk := range encodedChunks {
		s.logger.WithFields(logrus.Fields{
			"sha":      hashStr,
			"count":    totalChunks,
			"index":    i + 1,
			"content":  encodedChunk,
			"filename": filename,
		}).Info("screenshot chunk")
	}

	w.WriteHeader(http.StatusOK)
}

func splitIntoChunks(data []byte, chunkSize int) [][]byte {
	var chunks [][]byte
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, data[i:end])
	}
	return chunks
}
