package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
	"github.com/lmittmann/tint"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

type DatadogMetricDefinition struct {
	Spec struct {
		Query string `yaml:"query"`
	}
}

type MetricQueryError struct {
	HTTPResponse *http.Response // The HTTP resonse from the DD api
	NestedError  error          // The error we're returning
}

func (e *MetricQueryError) Error() string {
	return fmt.Sprintf("Error: %s", e.NestedError)
}

// QueryAnalysis contains information about a parsed query
type QueryAnalysis struct {
	OriginalQuery      string
	HasDefaultZero     bool
	InnerQuery         string
	DefaultZeroNesting int
}

// parseQuery analyzes a Datadog query to detect default_zero() usage and extract inner queries
func parseQuery(query string) *QueryAnalysis {
	analysis := &QueryAnalysis{
		OriginalQuery: query,
	}

	// Regular expression to match default_zero() function calls
	// This handles nested parentheses properly
	defaultZeroRegex := regexp.MustCompile(`^default_zero\s*\(`)

	if !defaultZeroRegex.MatchString(strings.TrimSpace(query)) {
		return analysis
	}

	analysis.HasDefaultZero = true

	// Extract the inner query by finding matching parentheses
	innerQuery, nesting := extractInnerQuery(query)
	analysis.InnerQuery = innerQuery
	analysis.DefaultZeroNesting = nesting

	return analysis
}

// extractInnerQuery extracts the inner query from default_zero() function calls
// Returns the inner query and the nesting level of default_zero calls
func extractInnerQuery(query string) (string, int) {
	trimmed := strings.TrimSpace(query)
	nesting := 0

	// Keep peeling off default_zero() layers
	for {
		defaultZeroRegex := regexp.MustCompile(`^default_zero\s*\((.+)\)$`)
		matches := defaultZeroRegex.FindStringSubmatch(trimmed)

		if len(matches) != 2 {
			break
		}

		nesting++
		inner := strings.TrimSpace(matches[1])

		// Check if the inner content is another default_zero call
		if !strings.HasPrefix(inner, "default_zero") {
			return inner, nesting
		}

		trimmed = inner
	}

	return trimmed, nesting
}

func main() {
	// We might want to have a cli option for log level, possibly.
	setupLogger("DEBUG")

	// `args` here is just a list of files
	flag.Parse()
	files := flag.Args()

	if len(files) == 0 {
		slog.Error("Please provide a list of files to process")
	}

	// configure the context with the required API auth tokens
	ctx := context.WithValue(
		context.Background(),
		datadog.ContextAPIKeys,
		map[string]datadog.APIKey{
			"apiKeyAuth": {
				Key: os.Getenv("DD_CLIENT_API_KEY"),
			},
			"appKeyAuth": {
				Key: os.Getenv("DD_CLIENT_APP_KEY"),
			},
		},
	)

	apiClient := datadog.NewAPIClient(datadog.NewConfiguration())
	api := datadogV1.NewMetricsApi(apiClient)

	failures := 0

	for _, file := range files {
		query, err := extractQuery(file)
		if err != nil {
			slog.Error("Error extracting query from file",
				slog.String("filename", file),
				slog.Any("err", err),
			)

			failures++

			continue
		}

		// The file was valid yaml, but didnt contain a `spec.query` field, so while it's technically invalid, this
		// shouldn't count as a failure for the linting process. Just move on and dont increment `failures`.
		if query == "" {
			slog.Warn("File didn't contain a metric query, skipping it", slog.String("filename", file))
			continue
		}

		// Analyze the query to detect default_zero usage
		analysis := parseQuery(query)

		// Always validate the original query first
		value, err := fetchMetric(ctx, api, query)

		var mqe *MetricQueryError
		if err != nil {
			if errors.As(err, &mqe) {
				slog.Error("Error calling `MetricsApi.Querymetrics`",
					slog.String("file", file),
					slog.String("query", query),
					slog.Any("err", mqe.NestedError),
				)
			}

			failures++
			continue
		}

		// If the original query has default_zero, we need additional validation
		if analysis.HasDefaultZero {
			slog.Debug("Query uses default_zero, validating inner query",
				slog.String("file", file),
				slog.String("original_query", analysis.OriginalQuery),
				slog.String("inner_query", analysis.InnerQuery),
				slog.Int("nesting_level", analysis.DefaultZeroNesting),
			)

			// Test the inner query without default_zero to see if it's actually valid
			innerValue, innerErr := fetchMetric(ctx, api, analysis.InnerQuery)

			if innerErr != nil {
				var innerMqe *MetricQueryError
				if errors.As(innerErr, &innerMqe) {
					slog.Error("Inner query validation failed - default_zero() is masking an invalid metric",
						slog.String("file", file),
						slog.String("original_query", analysis.OriginalQuery),
						slog.String("inner_query", analysis.InnerQuery),
						slog.Any("err", innerMqe.NestedError),
					)
					failures++
					continue
				}
			}

			// Check if inner query returns no data (potential invalid metric)
			if innerValue == nil {
				slog.Warn("Inner query returns no data - metric may not exist but default_zero() masks this",
					slog.String("file", file),
					slog.String("original_query", analysis.OriginalQuery),
					slog.String("inner_query", analysis.InnerQuery),
				)
				// This is a warning, not a hard failure, as the metric might legitimately have no current data
			}
		}

		if value == nil {
			slog.Warn("Query returned no data; the metric might not be real or there may not be any datapoints",
				slog.String("file", file),
				slog.String("query", query),
			)
		} else {
			slog.Info("Query result",
				slog.String("file", file),
				slog.String("query", query),
				slog.Float64("value", *value),
			)
		}
	}

	if failures > 0 {
		os.Exit(failures)
	}
}

func setupLogger(logLevel string) {
	var level slog.Level

	switch logLevel {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := tint.NewHandler(os.Stdout, &tint.Options{
		AddSource:  false,
		Level:      level,
		TimeFormat: time.RFC3339,
	})
	logger := slog.New(handler)

	slog.SetDefault(logger)
}

// Load the yaml file, and extract `spec.query` from the data. This is the datadog query that needs to be
// validated, which is returned as a string.
func extractQuery(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", errors.Wrap(err, fmt.Sprintf("Failed to read file: %s", filePath))
	}

	var metric DatadogMetricDefinition

	err = yaml.Unmarshal(data, &metric)
	if err != nil {
		return "", errors.Wrap(err, fmt.Sprintf("Failed to unmarshal yaml: %s", filePath))
	}

	return metric.Spec.Query, nil
}

// Fetch the metric value for the specified query from the Datadog API, if possible.
func fetchMetric(ctx context.Context, api *datadogV1.MetricsApi, query string) (*float64, error) {
	fiveMinAgo := time.Now().Add(-1 * time.Minute).Unix()
	metricResp, httpResp, err := api.QueryMetrics(ctx, fiveMinAgo, time.Now().Unix(), query)

	switch {
	case err != nil:
		// HTTP error or some other lower level issue.
		mqe := &MetricQueryError{
			HTTPResponse: httpResp,
			NestedError:  err,
		}

		return nil, mqe

	case metricResp.Status != nil && *metricResp.Status == "error":
		// Error occurred in the API, so it's a bad query, bad auth, or something similar.
		mqe := &MetricQueryError{
			HTTPResponse: httpResp,
			NestedError:  fmt.Errorf("MetricResponseError: %v", *metricResp.Error),
		}

		return nil, mqe

	default:
		// The API call technically succeeded in that the query wasn't malformed.
		// Note that this doesn't mean the metric is necessarily a real metric, just that the query succeeded.
		if len(metricResp.Series) > 0 && metricResp.Series[0].End != nil {
			// Return the latest non-null value in the time series.
			series := metricResp.Series[0]
			for i := len(series.Pointlist) - 1; i >= 0; i-- {
				point := series.Pointlist[i]
				if point[1] != nil {
					return point[1], nil
				}
			}
		}

		// No time series returned or all points were null. Probably a metric w/out data or it doesn't exist.
		//nolint:nilnil
		return nil, nil
	}
}
