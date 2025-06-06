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

// MetricInfo contains information about an individual metric
type MetricInfo struct {
	OriginalMetric     string // The metric as it appears in the query (with default_zero if present)
	CleanMetric        string // The metric without default_zero wrapping
	HasDefaultZero     bool
	DefaultZeroNesting int
	StartPos           int // Position in the original query where this metric starts
	EndPos             int // Position in the original query where this metric ends
}

// QueryAnalysis contains information about a parsed query
type QueryAnalysis struct {
	OriginalQuery      string
	HasDefaultZero     bool
	InnerQuery         string        // Deprecated: use Metrics instead for multi-metric queries
	DefaultZeroNesting int           // Deprecated: use Metrics instead for multi-metric queries
	Metrics            []MetricInfo  // All metrics found in the query
	IsComplexQuery     bool          // True if query contains multiple metrics or mathematical operations
}

// parseQuery analyzes a Datadog query to detect default_zero() usage and extract all metrics
func parseQuery(query string) *QueryAnalysis {
	analysis := &QueryAnalysis{
		OriginalQuery: query,
		Metrics:       []MetricInfo{},
	}

	// Check if this is a simple query (single metric) or complex query (multiple metrics/operations)
	analysis.IsComplexQuery = isComplexQuery(query)

	if analysis.IsComplexQuery {
		// Parse multiple metrics from complex query
		metrics := extractAllMetrics(query)
		analysis.Metrics = metrics
		
		// Set legacy fields for backward compatibility
		if len(metrics) > 0 {
			analysis.HasDefaultZero = metrics[0].HasDefaultZero
			analysis.InnerQuery = metrics[0].CleanMetric
			analysis.DefaultZeroNesting = metrics[0].DefaultZeroNesting
		}
	} else {
		// Handle simple single-metric query (backward compatibility)
		trimmed := strings.TrimSpace(query)
		defaultZeroRegex := regexp.MustCompile(`^default_zero\s*\(`)
		
		if defaultZeroRegex.MatchString(trimmed) {
			analysis.HasDefaultZero = true
			innerQuery, nesting := extractInnerQuery(query)
			analysis.InnerQuery = innerQuery
			analysis.DefaultZeroNesting = nesting
			
			// Also populate the new Metrics field
			metric := MetricInfo{
				OriginalMetric:     query,
				CleanMetric:        innerQuery,
				HasDefaultZero:     true,
				DefaultZeroNesting: nesting,
				StartPos:           0,
				EndPos:             len(query),
			}
			analysis.Metrics = []MetricInfo{metric}
		} else {
			// Simple metric without default_zero
			metric := MetricInfo{
				OriginalMetric:     query,
				CleanMetric:        query,
				HasDefaultZero:     false,
				DefaultZeroNesting: 0,
				StartPos:           0,
				EndPos:             len(query),
			}
			analysis.Metrics = []MetricInfo{metric}
		}
	}

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

// isComplexQuery determines if a query contains multiple metrics or mathematical operations
func isComplexQuery(query string) bool {
	// Look for mathematical operators outside of metric definitions
	// Simple heuristic: if we find +, -, *, / outside of braces {}, it's likely a complex query
	inBraces := 0
	inParens := 0
	
	for i, char := range query {
		switch char {
		case '{':
			inBraces++
		case '}':
			inBraces--
		case '(':
			inParens++
		case ')':
			inParens--
		case '+', '-', '*', '/':
			// If we're not inside braces or function calls, this might be a mathematical operation
			if inBraces == 0 {
				// Check if this is actually a mathematical operator by looking at context
				if i > 0 && i < len(query)-1 {
					prevRune := rune(query[i-1])
					nextRune := rune(query[i+1])
					// Simple check: if surrounded by non-space characters or if it's clearly an operator
					if (prevRune != ' ' && nextRune != ' ') || 
					   (char == '+' || char == '-' || char == '*' || char == '/') {
						return true
					}
				}
			}
		}
	}
	
	// Also check for multiple metric patterns (avg:, sum:, count:, etc.)
	metricPrefixes := []string{"avg:", "sum:", "count:", "min:", "max:", "rate:", "gauge:"}
	metricCount := 0
	
	for _, prefix := range metricPrefixes {
		count := strings.Count(query, prefix)
		metricCount += count
	}
	
	return metricCount > 1
}

// extractAllMetrics finds all metrics in a complex query
func extractAllMetrics(query string) []MetricInfo {
	var metrics []MetricInfo
	
	// Use a more sophisticated approach to find metrics
	// Look for patterns like default_zero(...) or direct metric references
	
	// First, find all default_zero() calls
	defaultZeroMetrics := extractDefaultZeroMetrics(query)
	metrics = append(metrics, defaultZeroMetrics...)
	
	// Then find any remaining metrics that aren't wrapped in default_zero
	remainingMetrics := extractRemainingMetrics(query, metrics)
	metrics = append(metrics, remainingMetrics...)
	
	return metrics
}

// extractDefaultZeroMetrics finds all default_zero() wrapped metrics in the query
func extractDefaultZeroMetrics(query string) []MetricInfo {
	var metrics []MetricInfo
	
	// Regular expression to match default_zero function calls with proper nesting
	defaultZeroRegex := regexp.MustCompile(`default_zero\s*\(`)
	
	// Find all matches
	matches := defaultZeroRegex.FindAllStringIndex(query, -1)
	
	// Track which positions are already covered by outer default_zero calls
	coveredPositions := make(map[int]bool)
	
	for _, match := range matches {
		startPos := match[0]
		
		// Check if this match is already covered by a previous outer default_zero
		if coveredPositions[startPos] {
			continue
		}
		
		// Find the matching closing parenthesis
		parenCount := 0
		endPos := -1
		
		// Start from the opening parenthesis
		openParenPos := match[1] - 1 // Position of the opening '('
		
		for i := openParenPos; i < len(query); i++ {
			if query[i] == '(' {
				parenCount++
			} else if query[i] == ')' {
				parenCount--
				if parenCount == 0 {
					endPos = i + 1
					break
				}
			}
		}
		
		if endPos != -1 {
			fullMatch := query[startPos:endPos]
			innerQuery, nesting := extractInnerQuery(fullMatch)
			
			metric := MetricInfo{
				OriginalMetric:     fullMatch,
				CleanMetric:        innerQuery,
				HasDefaultZero:     true,
				DefaultZeroNesting: nesting,
				StartPos:           startPos,
				EndPos:             endPos,
			}
			metrics = append(metrics, metric)
			
			// Mark all positions within this metric as covered
			for i := startPos; i < endPos; i++ {
				coveredPositions[i] = true
			}
		}
	}
	
	return metrics
}

// extractRemainingMetrics finds metrics that aren't wrapped in default_zero
func extractRemainingMetrics(query string, existingMetrics []MetricInfo) []MetricInfo {
	var metrics []MetricInfo
	
	// Create a set of positions that are already covered by existing metrics
	coveredPositions := make(map[int]bool)
	for _, metric := range existingMetrics {
		for i := metric.StartPos; i < metric.EndPos; i++ {
			coveredPositions[i] = true
		}
	}
	
	// Look for metric patterns that aren't covered
	metricPattern := regexp.MustCompile(`(avg|sum|count|min|max|rate|gauge):[a-zA-Z0-9._]+(\{[^}]*\})?(\.[a-zA-Z0-9_()]+)*`)
	
	matches := metricPattern.FindAllStringIndex(query, -1)
	
	for _, match := range matches {
		startPos := match[0]
		endPos := match[1]
		
		// Check if this metric is already covered by a default_zero metric
		covered := false
		for i := startPos; i < endPos; i++ {
			if coveredPositions[i] {
				covered = true
				break
			}
		}
		
		if !covered {
			metricText := query[startPos:endPos]
			metric := MetricInfo{
				OriginalMetric:     metricText,
				CleanMetric:        metricText,
				HasDefaultZero:     false,
				DefaultZeroNesting: 0,
				StartPos:           startPos,
				EndPos:             endPos,
			}
			metrics = append(metrics, metric)
		}
	}
	
	return metrics
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

		// Analyze the query to detect default_zero usage and extract all metrics
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

		// Validate each individual metric found in the query
		if analysis.IsComplexQuery {
			slog.Debug("Complex query detected, validating individual metrics",
				slog.String("file", file),
				slog.String("original_query", query),
				slog.Int("metric_count", len(analysis.Metrics)),
			)

			for i, metric := range analysis.Metrics {
				if metric.HasDefaultZero {
					slog.Debug("Validating default_zero wrapped metric",
						slog.String("file", file),
						slog.Int("metric_index", i),
						slog.String("original_metric", metric.OriginalMetric),
						slog.String("clean_metric", metric.CleanMetric),
						slog.Int("nesting_level", metric.DefaultZeroNesting),
					)

					// Test the clean metric without default_zero to see if it's actually valid
					metricValue, metricErr := fetchMetric(ctx, api, metric.CleanMetric)

					if metricErr != nil {
						var metricMqe *MetricQueryError
						if errors.As(metricErr, &metricMqe) {
							slog.Error("Individual metric validation failed - default_zero() is masking an invalid metric",
								slog.String("file", file),
								slog.Int("metric_index", i),
								slog.String("original_metric", metric.OriginalMetric),
								slog.String("clean_metric", metric.CleanMetric),
								slog.Any("err", metricMqe.NestedError),
							)
							failures++
							continue
						}
					}

					// Check if metric returns no data (potential invalid metric)
					if metricValue == nil {
						slog.Warn("Individual metric returns no data - metric may not exist but default_zero() masks this",
							slog.String("file", file),
							slog.Int("metric_index", i),
							slog.String("original_metric", metric.OriginalMetric),
							slog.String("clean_metric", metric.CleanMetric),
						)
					}
				} else {
					// For metrics without default_zero, just validate them directly
					slog.Debug("Validating non-default_zero metric",
						slog.String("file", file),
						slog.Int("metric_index", i),
						slog.String("metric", metric.CleanMetric),
					)

					metricValue, metricErr := fetchMetric(ctx, api, metric.CleanMetric)

					if metricErr != nil {
						var metricMqe *MetricQueryError
						if errors.As(metricErr, &metricMqe) {
							slog.Error("Individual metric validation failed",
								slog.String("file", file),
								slog.Int("metric_index", i),
								slog.String("metric", metric.CleanMetric),
								slog.Any("err", metricMqe.NestedError),
							)
							failures++
							continue
						}
					}

					if metricValue == nil {
						slog.Warn("Individual metric returns no data - metric may not exist",
							slog.String("file", file),
							slog.Int("metric_index", i),
							slog.String("metric", metric.CleanMetric),
						)
					}
				}
			}
		} else if analysis.HasDefaultZero {
			// Handle simple single-metric query with default_zero (backward compatibility)
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
