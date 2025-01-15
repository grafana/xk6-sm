package sm

import (
	"io"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.k6.io/k6/metrics"
	"go.k6.io/k6/output"
)

func TestOutputNew(t *testing.T) {
	t.Parallel()

	testcases := map[string]struct {
		input       output.Params
		expectError bool
	}{
		"happy path": {
			input:       output.Params{ConfigArgument: "test.out", FS: afero.NewMemMapFs(), Logger: nopLogger()},
			expectError: false,
		},
		"no filename": {
			input:       output.Params{ConfigArgument: "", FS: afero.NewMemMapFs(), Logger: nopLogger()},
			expectError: true,
		},
		"cannot create file": {
			input:       output.Params{ConfigArgument: "test.out", FS: afero.NewReadOnlyFs(afero.NewMemMapFs()), Logger: nopLogger()},
			expectError: true,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			actual, err := New(tc.input)
			if tc.expectError {
				require.Error(t, err)
				require.Nil(t, actual)
			} else {
				require.NoError(t, err)
				require.NotNil(t, actual)
			}
		})
	}
}

func TestOutputDescription(t *testing.T) {
	t.Parallel()

	var out Output
	require.NotEmpty(t, out.Description())
}

func TestOutputStartStop(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	out, err := New(output.Params{ConfigArgument: "test.out", FS: fs, Logger: nopLogger()})
	require.NoError(t, err)

	err = out.Start()
	require.NoError(t, err)

	err = out.Stop()
	require.NoError(t, err)

	fileOut, err := fs.Open("test.out")
	require.NoError(t, err)

	output, err := io.ReadAll(fileOut)
	require.NoError(t, err)

	require.Contains(t, string(output), "probe_script_duration_seconds")
}

// TestMetricStore tests the metricStore functionality that is hard to test from an integration perspective.
func TestMetricStore(t *testing.T) {
	t.Parallel()

	t.Run("aggregation", func(t *testing.T) {
		t.Parallel()

		store := newMetricStore(8)

		trendA := metrics.TimeSeries{
			Metric: &metrics.Metric{
				Name: "im_trend_a",
				Type: metrics.Trend,
			},
			Tags: nil,
		}
		trendB := metrics.TimeSeries{
			Metric: &metrics.Metric{
				Name: "im_trend_b",
				Type: metrics.Trend,
			},
			Tags: nil,
		}

		gaugeA := metrics.TimeSeries{
			Metric: &metrics.Metric{
				Name: "im_gauge_a",
				Type: metrics.Gauge,
			},
			Tags: nil,
		}
		gaugeB := metrics.TimeSeries{
			Metric: &metrics.Metric{
				Name: "im_gaguge_b",
				Type: metrics.Gauge,
			},
			Tags: nil,
		}

		counterA := metrics.TimeSeries{
			Metric: &metrics.Metric{
				Name: "im_counter_a",
				Type: metrics.Counter,
			},
			Tags: nil,
		}
		counterB := metrics.TimeSeries{
			Metric: &metrics.Metric{
				Name: "im_counter_b",
				Type: metrics.Counter,
			},
			Tags: nil,
		}

		store.Record(metrics.Sample{TimeSeries: trendA, Value: 1})
		store.Record(metrics.Sample{TimeSeries: trendB, Value: 1})
		store.Record(metrics.Sample{TimeSeries: trendA, Value: 2})

		store.Record(metrics.Sample{TimeSeries: gaugeA, Value: 1})
		store.Record(metrics.Sample{TimeSeries: gaugeB, Value: 1})
		store.Record(metrics.Sample{TimeSeries: gaugeA, Value: 2})

		store.Record(metrics.Sample{TimeSeries: counterA, Value: 1})
		store.Record(metrics.Sample{TimeSeries: counterB, Value: 1})
		store.Record(metrics.Sample{TimeSeries: counterA, Value: 2})

		assert.Equal(t, 1.5, store.store[timeseriesFromK6(trendA)].value)
		assert.Equal(t, 2, store.store[timeseriesFromK6(trendA)].seenSamples)
		assert.Equal(t, 1.0, store.store[timeseriesFromK6(trendB)].value)
		assert.Equal(t, 1, store.store[timeseriesFromK6(trendB)].seenSamples)

		assert.Equal(t, 2.0, store.store[timeseriesFromK6(gaugeA)].value)
		assert.Equal(t, 2, store.store[timeseriesFromK6(gaugeA)].seenSamples)
		assert.Equal(t, 1.0, store.store[timeseriesFromK6(gaugeB)].value)
		assert.Equal(t, 1, store.store[timeseriesFromK6(gaugeB)].seenSamples)

		assert.Equal(t, 3.0, store.store[timeseriesFromK6(counterA)].value)
		assert.Equal(t, 2, store.store[timeseriesFromK6(counterA)].seenSamples)
		assert.Equal(t, 1.0, store.store[timeseriesFromK6(counterB)].value)
		assert.Equal(t, 1, store.store[timeseriesFromK6(counterB)].seenSamples)
	})
}

func TestSanitizeLabelName(t *testing.T) {
	t.Parallel()

	testcases := map[string]struct {
		input    string
		expected string
	}{
		"single letter":         {input: "a", expected: "a"},
		"word":                  {input: "abc", expected: "abc"},
		"letter and number":     {input: "a1", expected: "a1"},
		"number":                {input: "1", expected: "_"},
		"numbers":               {input: "123", expected: "_23"},
		"underscore":            {input: "_", expected: "_"},
		"valid with underscore": {input: "a_b_c", expected: "a_b_c"},
		"valid with numbers":    {input: "a_1_2", expected: "a_1_2"},
		"colon":                 {input: ":", expected: ":"},
		"namespace":             {input: "abc::xyz", expected: "abc::xyz"},
		"blank":                 {input: " ", expected: "_"},
		"words with blank":      {input: "abc xyz", expected: "abc_xyz"},
		"dash":                  {input: "-", expected: "_"},
		"words with dash":       {input: "abc-xyz", expected: "abc_xyz"},
		"utf8":                  {input: "á", expected: "_"},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			actual := sanitizeLabelName(tc.input)
			if actual != tc.expected {
				t.Log("expected:", tc.expected, "actual:", actual, "input:", tc.input)
				t.Fail()
			}
		})
	}
}

func nopLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)

	return l
}
