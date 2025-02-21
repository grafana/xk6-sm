package sm

import (
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mstoykov/atlas"
	"github.com/sirupsen/logrus"
	"go.k6.io/k6/metrics"
	"go.k6.io/k6/output"
)

const (
	ExtensionName = "sm"
)

func init() {
	output.RegisterExtension(ExtensionName, New)
}

// Value stores the value of a timeseries, after aggregation.
// Samples are aggregated into a single value as they arrive, as in SM context we are not interested in keeping more
// than one datapoint per timeseries.
type value struct {
	// value contains the numeric value of the metric, after aggregation.
	value float64
	// seenSamples stores the number of samples seen from the metric. This is used to perform averages in constant
	// space, that is, without storing the full list of seen samples.
	seenSamples int
}

// timeseries is a simplified version of k6 [metrics.TimeSeries].
// It can be used as a map key for the same reason [metrics.TimeSeries] can: [metrics.TagSet] is immutable (modifying it
// clones it and returns a new pointer), and k6 promises to always reuse the same TagSet instance instance for each
// given timeseries whose TagSet contents are the same.
type timeseries struct {
	name       string
	metricType metrics.MetricType
	tags       *metrics.TagSet
}

// timeseriesFromK6 simplifies k6's [metrics.TimeSeries] into timeseries.
//
// Equality of the arguments is preserved: If t1 t2 are of type [metrics.TimeSeries], and t1 == t2, then
// timeseriesFromK6(t1) == timeseriesFromK6(t2).
func timeseriesFromK6(k6Ts metrics.TimeSeries) timeseries {
	return timeseries{
		name:       k6Ts.Metric.Name,
		metricType: k6Ts.Metric.Type,
		tags:       k6Ts.Tags,
	}
}

// metricStore stores and processes k6 samples according to SM needs. It can aggregate samples as they arrive in
// constant memory, and peform post-processing (metric renaming and manipulation) when k6 is done executing and all
// samples have been aggregated.
//
// Post-processing does four things:
// - Derive new timeseries from existing ones, in a 1:N mapping. This includes cloning timeseries under new names,
// changing units, or creating new metrics from labels, for example.
// - Derive logs from timeseries.
// - Remove specific timeseries.
// - Remove specific labels from specific timeseries.
//
// Post-processing implemented here cannot do:
// - Promql-like aggregations, e.g. aggregate multiple timeseries into one
// - N:M or N:1 metric mappings
//
// TODO: We need to store samples on a map in order to perform the aggregation efficiently, but for the post-processing
// step, where the map of metrics is simply walked through, a slice would be faster. Converting the map to slice before
// post-processing would be a good performance optimization to make here.
type metricStore struct {
	logger logrus.FieldLogger
	mtx    sync.Mutex
	store  map[timeseries]value
}

func newMetricStore(size int) *metricStore {
	logger := logrus.New()
	logger.Out = io.Discard

	return &metricStore{
		store:  make(map[timeseries]value, size),
		logger: logger,
	}
}

func (ms *metricStore) Len() int {
	return len(ms.store)
}

// Record inserts a new sample into the store in a thread-safe way, aggregating it if the timeseries was already present
// in the metricStore.
// Record locks a mutex, and thus should be as fast as k6's SampleBuffer.
func (ms *metricStore) Record(sample metrics.Sample) {
	log := ms.logger.WithField("step", "record")

	ts := timeseriesFromK6(sample.TimeSeries)

	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	old, found := ms.store[ts]
	if !found {
		// Timeseries not already in store, just add it.
		ms.store[ts] = value{
			value:       sample.Value,
			seenSamples: 1,
		}

		return
	}

	log.Tracef("Aggregating sample (%f) for %q into existing (%f) value", sample.Value, ts.name, old.value)

	updated := old
	switch ts.metricType {
	case metrics.Counter:
		// Sum values.
		updated.value += sample.Value

	case metrics.Trend, metrics.Rate:
		// Compute the average.
		updated.value = ((old.value * float64(old.seenSamples)) + sample.Value) / (float64(old.seenSamples) + 1)

	case metrics.Gauge:
		// Replace with newest.
		updated.value = sample.Value
	default:
		log.Tracef("Unknown metric type %q for %q, keeping previous sample without aggregating", ts.metricType, ts.name)
		return
	}

	updated.seenSamples++
	ms.store[ts] = updated
}

// DeriveMetrics creates new metrics from existing ones. These metrics are created either to have some consistency with
// other SM checks, or as a preparation step to set labels on them that will then be removed from others.
// Metrics are derived sequentially, on a single pass. This means that DeriveMetrics can only be extended to derive
// metrics in a 1:N fashion, where one existing metric produces N derived metrics. DeriveMetrics cannot aggregate
// multiple metrics into one.
func (ms *metricStore) DeriveMetrics() {
	log := ms.logger.WithField("step", "deriveMetrics")

	for ts, v := range ms.store {
		// Range over the existing metrics and create new ones, adding them to the map on the fly. This is safe as per
		// the go spec, with the only caveat that whether new values will be iterated over is undefined. We do not care
		// about that.
		// We need to range over the map instead of fetching these metrics directly, as each metric may appear multiple
		// time with different labels (e.g. different URLs).
		// Inline funcs are used to scope variables and avoid copy-paste bugs.
		switch ts.name {
		// Create specific metrics containing info about http calls.
		// Additionally, clone this metric as http_requests_total.
		case "http_reqs":
			func() {
				renamedTS := timeseries{
					name:       "http_requests_total",
					metricType: metrics.Counter,
					tags:       ts.tags,
				}
				ms.store[renamedTS] = v
				log.Tracef("Created %q from %q", renamedTS.name, ts.name)
			}()

			func() {
				tags := ts.tags
				if tlsVersion, found := ts.tags.Get("tls_version"); found {
					tags = ts.tags.Without("tls_version").With("tls_version", strings.TrimPrefix(tlsVersion, "tls"))
				}
				infoTS := timeseries{
					name:       "http_info",
					metricType: metrics.Gauge,
					tags:       tags,
				}
				ms.store[infoTS] = value{1, 1}
				log.Tracef("Created %q from %q", infoTS.name, ts.name)
			}()

			func() {
				newValue := 0.0
				if _, found := ts.tags.Get("tls_version"); found {
					newValue = 1.0
				}

				sslTS := timeseries{
					name:       "http_ssl",
					metricType: metrics.Gauge,
					tags:       ts.tags,
				}
				ms.store[sslTS] = value{newValue, 1}
				log.Tracef("Created %q from %q", sslTS.name, ts.name)
			}()

			func() {
				newValue := 0.0
				if expected, _ := ts.tags.Get("expected_response"); expected == "true" {
					newValue = 1.0
				}

				responseTS := timeseries{
					name:       "http_got_expected_response",
					metricType: metrics.Gauge,
					tags:       ts.tags,
				}
				ms.store[responseTS] = value{newValue, 1}
				log.Tracef("Created %q from %q", responseTS.name, ts.name)
			}()

			func() {
				strCode, _ := ts.tags.Get("error_code")
				newValue, _ := strconv.ParseFloat(strCode, 32)
				errorCodeTS := timeseries{
					name:       "http_error_code",
					metricType: metrics.Gauge,
					tags:       ts.tags,
				}
				ms.store[errorCodeTS] = value{newValue, 1}
				log.Tracef("Created %q from %q", errorCodeTS.name, ts.name)
			}()

			// TODO: We should revisit this. This keeps the old behavior, but I'm not sure having the status code as the
			// value of a gauge is actually useful.
			func() {
				strCode, _ := ts.tags.Get("status")
				newValue, _ := strconv.ParseFloat(strCode, 32)
				statusCodeTS := timeseries{
					name:       "http_status_code",
					metricType: metrics.Gauge,
					tags:       ts.tags,
				}
				ms.store[statusCodeTS] = value{newValue, 1}
				log.Tracef("Created %q from %q", statusCodeTS.name, ts.name)
			}()

			func() {
				strCode, _ := ts.tags.Get("proto")
				newValue, _ := strconv.ParseFloat(strings.TrimPrefix(strCode, "HTTP/"), 32)
				httpVersionTS := timeseries{
					name:       "http_version",
					metricType: metrics.Gauge,
					tags:       ts.tags,
				}
				ms.store[httpVersionTS] = value{newValue, 1}
				log.Tracef("Created %q from %q", httpVersionTS.name, ts.name)
			}()

		// Rename to http_requests_failed_total
		case "http_req_failed":
			newTS := ts
			newTS.name = "http_requests_failed_total"
			ms.store[newTS] = v
			log.Tracef("Created %q from %q", newTS.name, ts.name)

		// Add _bytes suffix to data_sent and data_received.
		case "data_sent", "data_received":
			wihtSuffixTS := ts
			wihtSuffixTS.name = wihtSuffixTS.name + "_bytes"
			ms.store[wihtSuffixTS] = v
			log.Tracef("Created %q from %q", wihtSuffixTS.name, ts.name)

		case "http_req_duration":
			// Tweak name and units.
			func() {
				newTS := ts
				newTS.name = "http_total_duration_seconds"
				v.value /= 1000 // convert from ms.
				ms.store[newTS] = v
				log.Tracef("Created %q from %q", newTS.name, ts.name)
			}()
			// Additionally, use the labels of this metric to create a made up "resolve" phase with value of zero.
			func() {
				newTS := ts
				newTS.name = "http_duration_seconds"
				newTS.tags = newTS.tags.With("phase", "resolve")
				v.value = 0
				ms.store[newTS] = v
				log.Tracef("Created %s{phase=%q} from %q", newTS.name, "resolve", ts.name)
			}()

		case "iteration_duration":
			newTS := ts
			newTS.name = "iteration_duration_seconds"
			v.value /= 1000 // convert from ms.
			ms.store[newTS] = v
			log.Tracef("Created %q from %q", newTS.name, ts.name)

		// Squash multiple duration metrics into one with a "phase" label, which for historical reasons have slightly
		// different names to k6 phases.
		// Note that SM also outputs a http_duration_seconds{phase="resolve"} metric, but this one is hardcoded to zero
		// and generated from http_requ_duration.
		case "http_req_blocked", "http_req_connecting", "http_req_receiving", "http_req_sending", "http_req_tls_handshaking", "http_req_waiting":
			phase := strings.TrimPrefix(ts.name, "http_req_")
			switch phase {
			case "connecting":
				phase = "connect"
			case "tls_handshaking":
				phase = "tls"
			case "waiting":
				phase = "processing"
			case "receiving":
				phase = "transfer"
			}

			newTS := ts
			newTS.name = "http_duration_seconds"
			newTS.tags = newTS.tags.With("phase", phase)
			v.value /= 1000 // convert from ms.
			ms.store[newTS] = v
			log.Tracef("Created %s{phase=%q} from %q", newTS.name, phase, ts.name)

		// Split checks metric into two: check_rate and check_total.
		// TODO: We used to remove the "check" label due to it being high cardinality. However, we are reporting
		// metrics for URLs which are also high cardinality, so it does not make a lot of sense to remove one but not
		// others. For now, we're keeping the check metric.
		case "checks":
			func() {
				newTS := ts
				newTS.name = "check_success_rate"
				ms.store[newTS] = v
				log.Tracef("Created %q from %q", newTS.name, ts.name)
			}()

			// Create counters for the times a check failed and succeeded.
			// This is done by multiplying success rate by # of samples and rounding.
			func() {
				newTS := ts
				newTS.name = "checks_total"
				newTS.tags = newTS.tags.With("result", "pass")
				ms.store[newTS] = value{value: math.Round(v.value * float64(v.seenSamples)), seenSamples: 1}
				log.Tracef("Created %q from %q", newTS.name, ts.name)
			}()
			func() {
				newTS := ts
				newTS.name = "checks_total"
				newTS.tags = newTS.tags.With("result", "fail")
				ms.store[newTS] = value{value: math.Round((1 - v.value) * float64(v.seenSamples)), seenSamples: 1}
				log.Tracef("Created %q from %q", newTS.name, ts.name)
			}()
		}
	}
}

// DeriveLogs produces logs from metrics.
func (ms *metricStore) DeriveLogs(logger logrus.FieldLogger) {
	for ts, v := range ms.store {
		switch ts.name {
		// Checks contains the number of checks performed and the rate of them that succeeded.
		case "checks":
			tags := ts.tags.Map()
			if tags["group"] == "" {
				// Be consistent with metrics, and ignore "group" tag if empty.
				delete(tags, "group")
			}

			checkLogger := logger
			for k, v := range tags {
				checkLogger = checkLogger.WithField(k, v)
			}
			checkLogger.
				WithField("value", v.value).
				WithField("count", v.seenSamples).
				WithField("metric", "checks_total").
				Info("check result")
		}
	}
}

// RemoveLabels returns a new metricStore after removing labels not interesting for SM from all, or some metrics in the
// store.
func (ms *metricStore) RemoveLabels() {
	log := ms.logger.WithField("step", "removeLabels")

	// When we remove labels, we create a new TS without the label and store it on the map. As the new TS would have the
	// same name, we cannot store it on the same map we're iterating over, or we could risk iterating over the newly
	// added key. We need to duplicate the map for this.
	newStore := make(map[timeseries]value, len(ms.store))

	for ts, v := range ms.store {
		if ts.name != "http_info" {
			// We derived this metric earlier. Now we remove these tags from every other.
			log.Tracef("Removing http info labels from %q", ts.name)
			ts.tags = ts.tags.Without("tls_version")
			ts.tags = ts.tags.Without("proto")
		}

		if ts.name != "http_requests_total" {
			// Keep status label only on total requests.
			log.Tracef("Removing status label from %q", ts.name)
			ts.tags = ts.tags.Without("status")
		}

		// The documentation at https://k6.io/docs/using-k6/tags-and-groups/ seems to suggest that
		// "group" should not be empty (it shouldn't be there if there's a single group), but I keep
		// seeing instances of an empty group name.
		if group, found := ts.tags.Get("group"); found && group == "" {
			log.Tracef("Removing empty group label from %q", ts.name)
			ts.tags = ts.tags.Without("group")
		}

		// Moved to dedicated metrics as values instead of tags.
		ts.tags = ts.tags.Without("error_code")
		ts.tags = ts.tags.Without("expected_response")

		// High cardinality label. This is already present in logs.
		ts.tags = ts.tags.Without("error")

		newStore[ts] = v
	}

	ms.store = newStore
}

// RemoveMetrics removes metrics that are not interesting in SM context.
func (ms *metricStore) RemoveMetrics() {
	log := ms.logger.WithField("step", "removeMetrics")

	// As per BenchmarkRemove, it is better to store needles on a map. Experimentally, 8 needles or less are faster to
	// store and check against in a slice, but 16 or more are faster on a map. Slices likely stop being worth it when
	// the full slice does not fit on a cache line.
	deletable := map[string]bool{
		// Not useful in SM context:
		"vus":        true,
		"vus_max":    true,
		"iterations": true,
		// Replaced by version with _bytes suffix:
		"data_sent":     true,
		"data_received": true,
		// Renamed to _seconds.
		"http_req_duration":  true,
		"iteration_duration": true,
		// Squashed into a single metric with a phase label.
		"http_req_blocked": true, "http_req_connecting": true, "http_req_receiving": true, "http_req_sending": true, "http_req_tls_handshaking": true, "http_req_waiting": true,
		// Renamed to http_requests(_failed)_total.
		"http_reqs":       true,
		"http_req_failed": true,
		// Replaced by check_rate and checks_total
		"checks": true,
	}

	for ts := range ms.store {
		if deletable[ts.name] {
			delete(ms.store, ts)
			log.Tracef("Dropping %q", ts.name)
		}
	}
}

// Output is a k6 output plugin that writes metrics to an io.Writer in
// Prometheus text exposition format.
type Output struct {
	logger logrus.FieldLogger
	store  *metricStore
	out    io.WriteCloser
	start  time.Time
}

// New creates a new instance of the output.
func New(p output.Params) (output.Output, error) {
	fn := p.ConfigArgument
	if len(fn) == 0 {
		return nil, errors.New("output filename required")
	}

	fh, err := p.FS.Create(fn)
	if err != nil {
		return nil, err
	}

	logger := p.Logger.WithField("output", "sm")

	store := newMetricStore(32) // Reasonable size assumption.
	store.logger = logger.WithField("component", "store")

	return &Output{
		logger: logger,
		out:    fh,
		store:  store,
	}, nil
}

// Description returns a human-readable description of the output that will be
// shown in `k6 run`. For extensions it probably should include the version as
// well.
func (o *Output) Description() string {
	return "Synthetic Monitoring output"
}

// Start is called before the Engine tries to use the output and should be
// used for any long initialization tasks, as well as for starting a
// goroutine to asynchronously flush metrics to the output.
func (o *Output) Start() error {
	o.start = time.Now()
	o.logger.WithFields(logrus.Fields{
		"output": o.Description(),
		"ts":     o.start.UnixMilli(),
	}).Debug("starting output")

	return nil
}

// AddMetricSamples receives the latest metric samples from the Engine.
// k6 docs say:
// This method is called synchronously, so do not do anything blocking here
// that might take a long time. Preferably, just use the SampleBuffer or
// something like it to buffer metrics until they are flushed.
//
// Instead of using a SampleBuffer we, record samples in our metricStore directly. We estimate this is just as fast as
// SampleBuffer, which also has a lock, while using less memory as we aggregate as we go instead of storing all samples
// in memory.
func (o *Output) AddMetricSamples(containers []metrics.SampleContainer) {
	for _, samples := range containers {
		for _, sample := range samples.GetSamples() {
			o.store.Record(sample)
		}
	}
}

// Stop flushes all remaining metrics and finalize the test run.
func (o *Output) Stop() error {
	duration := time.Since(o.start)
	o.logger.WithFields(logrus.Fields{
		"output":   o.Description(),
		"duration": duration,
	}).Debug("stopping output")

	defer o.out.Close()

	o.store.Record(metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: &metrics.Metric{
				Name: "script_duration_seconds",
				Type: metrics.Gauge,
			},
			Tags: (*metrics.TagSet)(atlas.New()),
		},
		Value: float64(duration.Seconds()),
	})

	o.store.DeriveMetrics()
	o.store.DeriveLogs(o.logger)
	o.store.RemoveMetrics()
	o.store.RemoveLabels()

	for ts, value := range o.store.store {
		fmt.Fprintf(o.out, "probe_%s%s %f\n", sanitizeLabelName(ts.name), marshalPrometheus(ts.tags.Map()), value.value)
	}

	return nil
}

func marshalPrometheus(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}

	labelNames := make([]string, 0, len(labels))
	for k := range labels {
		labelNames = append(labelNames, k)
	}
	slices.Sort(labelNames)

	pairs := make([]string, 0, len(labelNames))
	for _, name := range labelNames {
		pairs = append(pairs, fmt.Sprintf("%s=%q", sanitizeLabelName(name), labels[name]))
	}

	return "{" + strings.Join(pairs, ",") + "}"
}

// sanitizeLabelName replaces all invalid characters in s with '_'.
func sanitizeLabelName(s string) string {
	var builder strings.Builder

	for i, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == ':' || (r >= '0' && r <= '9' && i > 0) {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('_')
		}
	}

	return builder.String()
}
