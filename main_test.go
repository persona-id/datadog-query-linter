package main

import (
	"strings"
	"testing"
)

func TestFileLoading(t *testing.T) {
	t.Run("validate that files load", func(t *testing.T) {
		query, err := extractQuery("tests/datadogmetric-working.yaml")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		expectedQuery := "default_zero(avg:rails.temporal.workflow_task.queue_time.avg{app:persona-web-temporal-worker-retention,env:production,region:us-central1,task_queue:retention}.fill(null))"
		if query != expectedQuery {
			t.Errorf("Expected query %q, got %q", expectedQuery, query)
		}
	})

	t.Run("error if the files don't exist", func(t *testing.T) {
		_, err := extractQuery("tests/datadogmetric-no-file.yaml")
		if err == nil {
			t.Fatalf("Expected an error but didn't receive one.")
		}

		expectedErr := "Failed to read file: tests/datadogmetric-no-file.yaml: open tests/datadogmetric-no-file.yaml: no such file or directory"
		if err.Error() != expectedErr {
			t.Fatalf("Expected error string `%s` but got `%v`.", expectedErr, err)
		}
	})

	t.Run("error if the yaml is invalid", func(t *testing.T) {
		_, err := extractQuery("tests/invalid-yaml.yaml")
		if err == nil {
			t.Fatalf("Exected an error unmarshaling yaml, but didn't receive one")
		}

		expectedErr := "line 1: cannot unmarshal !!str `Hello, ...` into main.DatadogMetricDefinition"
		if !strings.Contains(err.Error(), expectedErr) {
			t.Fatalf("Expected error string `%s` but got `%v`.", expectedErr, err)
		}
	})
}

func TestQueryParsing(t *testing.T) {
	tests := []struct {
		name              string
		query             string
		expectDefaultZero bool
		expectedInner     string
		expectedNesting   int
		expectComplex     bool
		expectedMetrics   int
	}{
		{
			name:              "simple query without default_zero",
			query:             "avg:system.cpu.user{*}",
			expectDefaultZero: false,
			expectedInner:     "",
			expectedNesting:   0,
			expectComplex:     false,
			expectedMetrics:   1,
		},
		{
			name:              "query with default_zero",
			query:             "default_zero(avg:system.cpu.user{*})",
			expectDefaultZero: true,
			expectedInner:     "avg:system.cpu.user{*}",
			expectedNesting:   1,
			expectComplex:     false,
			expectedMetrics:   1,
		},
		{
			name:              "query with nested default_zero",
			query:             "default_zero(default_zero(avg:system.cpu.user{*}))",
			expectDefaultZero: true,
			expectedInner:     "avg:system.cpu.user{*}",
			expectedNesting:   2,
			expectComplex:     false,
			expectedMetrics:   1,
		},
		{
			name:              "complex query with multiple metrics",
			query:             "avg:system.cpu.user{*} + avg:system.cpu.system{*}",
			expectDefaultZero: false,
			expectedInner:     "avg:system.cpu.user{*}",
			expectedNesting:   0,
			expectComplex:     true,
			expectedMetrics:   2,
		},
		{
			name:              "complex query with default_zero wrapped metrics",
			query:             "(default_zero(default_zero(avg:deeply.nested.valid.metric1{fake:tag}))+default_zero(default_zero(avg:deeply.nested.valid.metric2{fake:tag})))/default_zero(avg:deeply.nested.valid.metric3{fake:tag})",
			expectDefaultZero: true,
			expectedInner:     "avg:deeply.nested.valid.metric1{fake:tag}",
			expectedNesting:   2,
			expectComplex:     true,
			expectedMetrics:   3,
		},
		{
			name:              "mixed metrics with some default_zero",
			query:             "default_zero(avg:valid.metric{tag:value}) * sum:another.metric{env:prod} / count:third.metric{*}",
			expectDefaultZero: true,
			expectedInner:     "avg:valid.metric{tag:value}",
			expectedNesting:   1,
			expectComplex:     true,
			expectedMetrics:   3,
		},
		{
			name:              "complex query with default_zero",
			query:             "default_zero(avg:rails.temporal.workflow_task.queue_time.avg{app:persona-web-temporal-worker-retention,env:production,region:us-central1,task_queue:retention}.fill(null))",
			expectDefaultZero: true,
			expectedInner:     "avg:rails.temporal.workflow_task.queue_time.avg{app:persona-web-temporal-worker-retention,env:production,region:us-central1,task_queue:retention}.fill(null)",
			expectedNesting:   1,
			expectComplex:     false,
			expectedMetrics:   1,
		},
		{
			name:              "query with spaces around default_zero",
			query:             "default_zero( avg:system.cpu.user{*} )",
			expectDefaultZero: true,
			expectedInner:     "avg:system.cpu.user{*}",
			expectedNesting:   1,
			expectComplex:     false,
			expectedMetrics:   1,
		},
		{
			name:              "query starting with default_zero but not function call",
			query:             "default_zero_custom_function(avg:system.cpu.user{*})",
			expectDefaultZero: false,
			expectedInner:     "",
			expectedNesting:   0,
			expectComplex:     false,
			expectedMetrics:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := parseQuery(tt.query)

			if analysis.HasDefaultZero != tt.expectDefaultZero {
				t.Errorf("Expected HasDefaultZero=%v, got %v", tt.expectDefaultZero, analysis.HasDefaultZero)
			}

			if analysis.InnerQuery != tt.expectedInner {
				t.Errorf("Expected InnerQuery=%q, got %q", tt.expectedInner, analysis.InnerQuery)
			}

			if analysis.DefaultZeroNesting != tt.expectedNesting {
				t.Errorf("Expected DefaultZeroNesting=%d, got %d", tt.expectedNesting, analysis.DefaultZeroNesting)
			}

			if analysis.OriginalQuery != tt.query {
				t.Errorf("Expected OriginalQuery=%q, got %q", tt.query, analysis.OriginalQuery)
			}

			if analysis.IsComplexQuery != tt.expectComplex {
				t.Errorf("Expected IsComplexQuery=%v, got %v", tt.expectComplex, analysis.IsComplexQuery)
			}

			if len(analysis.Metrics) != tt.expectedMetrics {
				t.Errorf("Expected %d metrics, got %d", tt.expectedMetrics, len(analysis.Metrics))
			}
		})
	}
}

func TestExtractInnerQuery(t *testing.T) {
	tests := []struct {
		name            string
		query           string
		expectedInner   string
		expectedNesting int
	}{
		{
			name:            "single default_zero",
			query:           "default_zero(avg:system.cpu.user{*})",
			expectedInner:   "avg:system.cpu.user{*}",
			expectedNesting: 1,
		},
		{
			name:            "double nested default_zero",
			query:           "default_zero(default_zero(avg:system.cpu.user{*}))",
			expectedInner:   "avg:system.cpu.user{*}",
			expectedNesting: 2,
		},
		{
			name:            "triple nested default_zero",
			query:           "default_zero(default_zero(default_zero(avg:system.cpu.user{*})))",
			expectedInner:   "avg:system.cpu.user{*}",
			expectedNesting: 3,
		},
		{
			name:            "complex inner query",
			query:           "default_zero(sum:docker.containers.running{image_name:web}.as_count())",
			expectedInner:   "sum:docker.containers.running{image_name:web}.as_count()",
			expectedNesting: 1,
		},
		{
			name:            "query without default_zero",
			query:           "avg:system.cpu.user{*}",
			expectedInner:   "avg:system.cpu.user{*}",
			expectedNesting: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner, nesting := extractInnerQuery(tt.query)

			if inner != tt.expectedInner {
				t.Errorf("Expected inner query=%q, got %q", tt.expectedInner, inner)
			}

			if nesting != tt.expectedNesting {
				t.Errorf("Expected nesting=%d, got %d", tt.expectedNesting, nesting)
			}
		})
	}
}

func TestComplexQueryDetection(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		isComplex  bool
	}{
		{
			name:      "simple metric",
			query:     "avg:system.cpu.user{*}",
			isComplex: false,
		},
		{
			name:      "simple metric with default_zero",
			query:     "default_zero(avg:system.cpu.user{*})",
			isComplex: false,
		},
		{
			name:      "two metrics with addition",
			query:     "avg:system.cpu.user{*} + avg:system.cpu.system{*}",
			isComplex: true,
		},
		{
			name:      "complex expression with parentheses",
			query:     "(avg:metric1{*} + avg:metric2{*}) / sum:metric3{*}",
			isComplex: true,
		},
		{
			name:      "your example query",
			query:     "(default_zero(default_zero(avg:deeply.nested.valid.metric1{fake:tag}))+default_zero(default_zero(avg:deeply.nested.valid.metric2{fake:tag})))/default_zero(avg:deeply.nested.valid.metric3{fake:tag})",
			isComplex: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isComplexQuery(tt.query)
			if result != tt.isComplex {
				t.Errorf("Expected isComplex=%v, got %v for query: %s", tt.isComplex, result, tt.query)
			}
		})
	}
}

func TestExtractAllMetrics(t *testing.T) {
	tests := []struct {
		name            string
		query           string
		expectedMetrics []struct {
			originalMetric     string
			cleanMetric        string
			hasDefaultZero     bool
			defaultZeroNesting int
		}
	}{
		{
			name:  "simple addition",
			query: "avg:system.cpu.user{*} + avg:system.cpu.system{*}",
			expectedMetrics: []struct {
				originalMetric     string
				cleanMetric        string
				hasDefaultZero     bool
				defaultZeroNesting int
			}{
				{
					originalMetric:     "avg:system.cpu.user{*}",
					cleanMetric:        "avg:system.cpu.user{*}",
					hasDefaultZero:     false,
					defaultZeroNesting: 0,
				},
				{
					originalMetric:     "avg:system.cpu.system{*}",
					cleanMetric:        "avg:system.cpu.system{*}",
					hasDefaultZero:     false,
					defaultZeroNesting: 0,
				},
			},
		},
		{
			name:  "complex query with default_zero",
			query: "(default_zero(default_zero(avg:deeply.nested.valid.metric1{fake:tag}))+default_zero(default_zero(avg:deeply.nested.valid.metric2{fake:tag})))/default_zero(avg:deeply.nested.valid.metric3{fake:tag})",
			expectedMetrics: []struct {
				originalMetric     string
				cleanMetric        string
				hasDefaultZero     bool
				defaultZeroNesting int
			}{
				{
					originalMetric:     "default_zero(default_zero(avg:deeply.nested.valid.metric1{fake:tag}))",
					cleanMetric:        "avg:deeply.nested.valid.metric1{fake:tag}",
					hasDefaultZero:     true,
					defaultZeroNesting: 2,
				},
				{
					originalMetric:     "default_zero(default_zero(avg:deeply.nested.valid.metric2{fake:tag}))",
					cleanMetric:        "avg:deeply.nested.valid.metric2{fake:tag}",
					hasDefaultZero:     true,
					defaultZeroNesting: 2,
				},
				{
					originalMetric:     "default_zero(avg:deeply.nested.valid.metric3{fake:tag})",
					cleanMetric:        "avg:deeply.nested.valid.metric3{fake:tag}",
					hasDefaultZero:     true,
					defaultZeroNesting: 1,
				},
			},
		},
		{
			name:  "mixed default_zero and normal metrics",
			query: "default_zero(avg:valid.metric{tag:value}) * sum:another.metric{env:prod} / count:third.metric{*}",
			expectedMetrics: []struct {
				originalMetric     string
				cleanMetric        string
				hasDefaultZero     bool
				defaultZeroNesting int
			}{
				{
					originalMetric:     "default_zero(avg:valid.metric{tag:value})",
					cleanMetric:        "avg:valid.metric{tag:value}",
					hasDefaultZero:     true,
					defaultZeroNesting: 1,
				},
				{
					originalMetric:     "sum:another.metric{env:prod}",
					cleanMetric:        "sum:another.metric{env:prod}",
					hasDefaultZero:     false,
					defaultZeroNesting: 0,
				},
				{
					originalMetric:     "count:third.metric{*}",
					cleanMetric:        "count:third.metric{*}",
					hasDefaultZero:     false,
					defaultZeroNesting: 0,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := extractAllMetrics(tt.query)

			if len(metrics) != len(tt.expectedMetrics) {
				t.Errorf("Expected %d metrics, got %d", len(tt.expectedMetrics), len(metrics))
				for i, m := range metrics {
					t.Logf("Metric %d: %+v", i, m)
				}
				return
			}

			for i, expected := range tt.expectedMetrics {
				metric := metrics[i]

				if metric.CleanMetric != expected.cleanMetric {
					t.Errorf("Metric %d: Expected CleanMetric=%q, got %q", i, expected.cleanMetric, metric.CleanMetric)
				}

				if metric.HasDefaultZero != expected.hasDefaultZero {
					t.Errorf("Metric %d: Expected HasDefaultZero=%v, got %v", i, expected.hasDefaultZero, metric.HasDefaultZero)
				}

				if metric.DefaultZeroNesting != expected.defaultZeroNesting {
					t.Errorf("Metric %d: Expected DefaultZeroNesting=%d, got %d", i, expected.defaultZeroNesting, metric.DefaultZeroNesting)
				}
			}
		})
	}
}

func TestMultiMetricTestFiles(t *testing.T) {
	t.Run("extract query from multi-metric complex file", func(t *testing.T) {
		query, err := extractQuery("tests/datadogmetric-multi-metric-complex.yaml")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		expectedQuery := "(default_zero(default_zero(avg:deeply.nested.valid.metric1{fake:tag}))+default_zero(default_zero(avg:deeply.nested.valid.metric2{fake:tag})))/default_zero(avg:deeply.nested.valid.metric3{fake:tag})"
		if query != expectedQuery {
			t.Errorf("Expected query %q, got %q", expectedQuery, query)
		}

		// Test that the query is properly parsed as complex
		analysis := parseQuery(query)
		if !analysis.IsComplexQuery {
			t.Error("Expected query to be detected as complex")
		}

		if len(analysis.Metrics) != 3 {
			t.Errorf("Expected 3 metrics, got %d", len(analysis.Metrics))
		}

		// Verify all metrics have default_zero
		for i, metric := range analysis.Metrics {
			if !metric.HasDefaultZero {
				t.Errorf("Metric %d should have default_zero", i)
			}
		}
	})

	t.Run("extract query from multi-metric simple file", func(t *testing.T) {
		query, err := extractQuery("tests/datadogmetric-multi-metric-simple.yaml")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		expectedQuery := "avg:system.cpu.user{*} + avg:system.cpu.system{*}"
		if query != expectedQuery {
			t.Errorf("Expected query %q, got %q", expectedQuery, query)
		}

		// Test that the query is properly parsed as complex
		analysis := parseQuery(query)
		if !analysis.IsComplexQuery {
			t.Error("Expected query to be detected as complex")
		}

		if len(analysis.Metrics) != 2 {
			t.Errorf("Expected 2 metrics, got %d", len(analysis.Metrics))
		}

		// Verify no metrics have default_zero
		for i, metric := range analysis.Metrics {
			if metric.HasDefaultZero {
				t.Errorf("Metric %d should not have default_zero", i)
			}
		}
	})

	t.Run("extract query from mixed metrics file", func(t *testing.T) {
		query, err := extractQuery("tests/datadogmetric-mixed-metrics.yaml")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		expectedQuery := "default_zero(avg:valid.metric{tag:value}) * sum:another.metric{env:prod} / count:third.metric{*}"
		if query != expectedQuery {
			t.Errorf("Expected query %q, got %q", expectedQuery, query)
		}

		// Test that the query is properly parsed as complex
		analysis := parseQuery(query)
		if !analysis.IsComplexQuery {
			t.Error("Expected query to be detected as complex")
		}

		if len(analysis.Metrics) != 3 {
			t.Errorf("Expected 3 metrics, got %d", len(analysis.Metrics))
		}

		// Verify first metric has default_zero, others don't
		if !analysis.Metrics[0].HasDefaultZero {
			t.Error("First metric should have default_zero")
		}
		if analysis.Metrics[1].HasDefaultZero {
			t.Error("Second metric should not have default_zero")
		}
		if analysis.Metrics[2].HasDefaultZero {
			t.Error("Third metric should not have default_zero")
		}
	})
}

func TestDefaultZeroTestFiles(t *testing.T) {
	t.Run("extract query from default_zero invalid metric file", func(t *testing.T) {
		query, err := extractQuery("tests/datadogmetric-default-zero-invalid.yaml")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		expectedQuery := "default_zero(avg:completely.invalid.metric.name{nonexistent:tag,invalid:filter})"
		if query != expectedQuery {
			t.Errorf("Expected query %q, got %q", expectedQuery, query)
		}

		// Test that the query is properly parsed
		analysis := parseQuery(query)
		if !analysis.HasDefaultZero {
			t.Error("Expected query to be detected as having default_zero")
		}

		expectedInner := "avg:completely.invalid.metric.name{nonexistent:tag,invalid:filter}"
		if analysis.InnerQuery != expectedInner {
			t.Errorf("Expected inner query %q, got %q", expectedInner, analysis.InnerQuery)
		}
	})

	t.Run("extract query from nested default_zero invalid metric file", func(t *testing.T) {
		query, err := extractQuery("tests/datadogmetric-nested-default-zero-invalid.yaml")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		expectedQuery := "default_zero(default_zero(avg:deeply.nested.invalid.metric{fake:tag}))"
		if query != expectedQuery {
			t.Errorf("Expected query %q, got %q", expectedQuery, query)
		}

		// Test that the query is properly parsed with nesting
		analysis := parseQuery(query)
		if !analysis.HasDefaultZero {
			t.Error("Expected query to be detected as having default_zero")
		}

		expectedInner := "avg:deeply.nested.invalid.metric{fake:tag}"
		if analysis.InnerQuery != expectedInner {
			t.Errorf("Expected inner query %q, got %q", expectedInner, analysis.InnerQuery)
		}

		if analysis.DefaultZeroNesting != 2 {
			t.Errorf("Expected nesting level 2, got %d", analysis.DefaultZeroNesting)
		}
	})

	t.Run("extract query from default_zero malformed syntax file", func(t *testing.T) {
		query, err := extractQuery("tests/datadogmetric-default-zero-malformed.yaml")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		expectedQuery := "default_zero(avg:metric.name{tag:value}.invalid_function_call())"
		if query != expectedQuery {
			t.Errorf("Expected query %q, got %q", expectedQuery, query)
		}

		// Test that the query is properly parsed
		analysis := parseQuery(query)
		if !analysis.HasDefaultZero {
			t.Error("Expected query to be detected as having default_zero")
		}

		expectedInner := "avg:metric.name{tag:value}.invalid_function_call()"
		if analysis.InnerQuery != expectedInner {
			t.Errorf("Expected inner query %q, got %q", expectedInner, analysis.InnerQuery)
		}
	})
}

// TODO: figure out how to mock calls to datadog so we don't need to use our API keys in the tests.
func TestMetricFetching(t *testing.T) {
	t.SkipNow()
}
