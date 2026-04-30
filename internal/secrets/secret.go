// Copyright (C) 2025-2026 Grafana Labs.
// SPDX-License-Identifier: AGPL-3.0-only

package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	gsmClient "github.com/grafana/gsm-api-go-client"
	"github.com/sirupsen/logrus"
	"go.k6.io/k6/secretsource"
	"golang.org/x/time/rate"
)

var (
	errInvalidConfig                 = errors.New("config parameter is required in format 'config=path/to/config'")
	errMissingURL                    = errors.New("url is required in config file")
	errMissingToken                  = errors.New("token is required in config file")
	errFailedToGetSecret             = errors.New("failed to get secret")
	errInvalidRequestsPerMinuteLimit = errors.New("requestsPerMinuteLimit must be greater than 0")
	errInvalidRequestsBurst          = errors.New("requestsBurst must be greater than 0")
	errRetryRequest                  = errors.New("retry request")
	errTooManyRetries                = errors.New("too many retries")
)

const (
	maxAttempts         = 5
	initialInterval     = 100 * time.Millisecond
	maxElapsedTime      = 2500 * time.Millisecond
	requestTimeout      = maxElapsedTime
	backoffMultiplier   = 2.5
	randomizationFactor = 0.10
)

// extConfig holds the configuration for Grafana Secrets.
type extConfig struct {
	URL                    string `json:"url"`
	Token                  string `json:"token"`
	RequestsPerMinuteLimit *int   `json:"requestsPerMinuteLimit"`
	RequestsBurst          *int   `json:"requestsBurst"`
}

func parseConfigArgument(configArg string) (string, error) {
	configKey, configPath, ok := strings.Cut(configArg, "=")
	if !ok || configKey != "config" {
		return "", errInvalidConfig
	}

	return configPath, nil
}

const (
	// The rate limiter replenishes tokens in the bucket once every 200 ms
	// (5 per second) and allows a burst of 10 requests firing faster than
	// that. If the client keeps making requests at the rapid pace, they
	// will be slowed down. This allows a client to ask for a bunch of
	// secrets at the start of a script, and then it slows it down to a
	// reasonable pace. A single script using more than 25 secrets is
	// probably a bad idea anyway. The rate limit is here to protect the
	// Grafana Secrets API from being hammered too hard by bugs in a
	// script (for example, someone using a secret inside a loop).
	//
	// These values can be adjusted as needed.
	defaultRequestsPerMinuteLimit = 300 // 300 requests per minute is one request every 200 ms
	defaultRequestsBurst          = 10  // Allow a burst of 10 requests
)

// EntryPoint returns an instance of grafanaSecrets that implements the
// secretsource.Source interface.
//
// It is expected that k6 is run with:
//
// --secret-source=grafanasecrets=config=filename.json
//
// config points to a filename containing the details to connect to the GSM
// API.
//
//nolint:ireturn // This is the interface that must be implemented.
func EntryPoint(params secretsource.Params) (secretsource.Source, error) {
	config, err := getConfig(params.ConfigArgument)
	if err != nil {
		return nil, fmt.Errorf("missing or invalid config: %w", err)
	}

	client, err := gsmClient.NewClient(config.URL, gsmClient.WithBearerAuth(config.Token))
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return &grafanaSecrets{
		client:  client,
		limiter: newLimiter(*config.RequestsPerMinuteLimit, *config.RequestsBurst),
		logger:  params.Logger,
	}, nil
}

type grafanaSecrets struct {
	client  *gsmClient.Client
	limiter limiter
	logger  logrus.FieldLogger
}

func (gs *grafanaSecrets) Name() string {
	return "Grafana Secrets"
}

func (gs *grafanaSecrets) Description() string {
	return "Grafana secrets for k6"
}

func (gs *grafanaSecrets) Get(key string) (string, error) {
	logger := gs.logger.WithField("key", key)

	logger.Debug("Getting secret")

	ctx := context.Background()

	if err := gs.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter error: %w", err)
	}

	var (
		retryAfterError *backoff.RetryAfterError
		retryCount      int
	)

	plaintext, err := retry(ctx, maxAttempts, initialInterval, maxElapsedTime, gs.get(ctx, key, logger, &retryCount))
	switch {
	case err == nil:
		if retryCount > 1 {
			logger.WithField("requests", retryCount).Info("secret retrieved")
		}

		return plaintext, nil

	case errors.Is(err, errRetryRequest):
		// If this error gets back here it's because we retried too many times.
		logger.WithField("requests", retryCount).Error("too many retries getting secret")

		return "", errTooManyRetries

	case errors.As(err, &retryAfterError):
		// The server asked us to wait longer than our total retry budget.
		logger.WithError(err).WithField("requests", retryCount).Warn("Retry-After exceeds retry budget")

		return "", errTooManyRetries

	default:
		logger.WithError(err).WithField("requests", retryCount).Error("error getting secret")

		return "", err
	}
}

func (gs *grafanaSecrets) get(
	ctx context.Context,
	key string,
	logger logrus.FieldLogger,
	counter *int,
) func() (string, error) {
	return func() (string, error) {
		reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()

		*counter++

		response, err := gs.client.DecryptSecretById(reqCtx, key)
		if err != nil {
			// Network error, retry.
			logger.WithError(err).Debug("error getting secret")

			return "", fmt.Errorf("failed to get secret: %w", err)
		}

		defer response.Body.Close()

		switch response.StatusCode {
		case http.StatusOK:
			var decryptedSecret gsmClient.DecryptedSecret

			err := json.NewDecoder(response.Body).Decode(&decryptedSecret)
			if err != nil {
				logger.WithError(err).Error("error decoding secret")

				return "", backoff.Permanent(fmt.Errorf("failed to decode response: %w", err))
			}

			return decryptedSecret.Plaintext, nil

		case http.StatusTooManyRequests:
			// Retry if we got a valid Retry-After header.
			seconds, err := strconv.Atoi(response.Header.Get("Retry-After"))
			if err == nil {
				return "", backoff.RetryAfter(seconds)
			}

			fallthrough

		case http.StatusRequestTimeout,
			http.StatusConflict,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout:
			// Log the retry as a warning for troubleshooting purposes.
			logger.WithField("status", response.Status).Warn("Retrying request")

			return "", errRetryRequest

		default:
			// Anything else we don't know how to handle and therefore we won't retry it.
			return "", backoff.Permanent(fmt.Errorf("status code %d: %w", response.StatusCode, errFailedToGetSecret))
		}
	}
}

type limiter interface {
	Wait(ctx context.Context) error
}

func newLimiter(requestsPerMinuteLimit, requestsBurst int) *rate.Limiter {
	// The calculation below looks wrong because it seems like it's giving
	// n min²/req, but the first number is actually time unit/min, so the
	// units of the result are time unit/req, which is correct because it's
	// the interval of time after which a new token is replenished. In
	// other words, the units are time unit/token.
	tokenReplenishInterval := time.Minute / time.Duration(requestsPerMinuteLimit)

	return rate.NewLimiter(rate.Every(tokenReplenishInterval), requestsBurst)
}

func getConfig(arg string) (extConfig, error) {
	var config extConfig

	// Parse the ConfigArgument to get the config file path
	configPath, err := parseConfigArgument(arg)
	if err != nil {
		return config, err
	}

	configData, err := os.ReadFile(configPath)
	if err != nil {
		return config, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := json.Unmarshal(configData, &config); err != nil {
		return config, fmt.Errorf("failed to parse JSON config: %w", err)
	}

	if config.URL == "" {
		return config, errMissingURL
	}

	if config.Token == "" {
		return config, errMissingToken
	}

	if config.RequestsPerMinuteLimit == nil {
		requestsPerMinuteLimit := defaultRequestsPerMinuteLimit
		config.RequestsPerMinuteLimit = &requestsPerMinuteLimit
	}

	if config.RequestsBurst == nil {
		requestsBurst := defaultRequestsBurst
		config.RequestsBurst = &requestsBurst
	}

	if *config.RequestsPerMinuteLimit <= 0 {
		return config, errInvalidRequestsPerMinuteLimit
	}

	if *config.RequestsBurst <= 0 {
		return config, errInvalidRequestsBurst
	}

	return config, nil
}

// retry executes the provided function until it succeeds or the maximum number of attempts is reached.
func retry(
	ctx context.Context,
	maxAttempts uint,
	baseInterval, maxElapsedTime time.Duration,
	operation func() (string, error),
) (string, error) {
	// MaxInterval is capped to 1/maxAttempts of the total budget so all retries can fire before the deadline.
	expbackoff := backoff.ExponentialBackOff{
		InitialInterval:     baseInterval,
		MaxInterval:         maxElapsedTime / time.Duration(maxAttempts), //nolint:gosec // Small number.
		Multiplier:          backoffMultiplier,
		RandomizationFactor: randomizationFactor,
	}

	//nolint:wrapcheck // This is returning our own error from operation.
	return backoff.Retry(
		ctx,
		operation,
		backoff.WithBackOff(&expbackoff),
		backoff.WithMaxTries(maxAttempts),
		backoff.WithMaxElapsedTime(maxElapsedTime),
	)
}
