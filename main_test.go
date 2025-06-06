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
	}{
		{
			name:              "simple query without default_zero",
			query:             "avg:system.cpu.user{*}",
			expectDefaultZero: false,
			expectedInner:     "",
			expectedNesting:   0,
		},
		{
			name:              "query with default_zero",
			query:             "default_zero(avg:system.cpu.user{*})",
			expectDefaultZero: true,
			expectedInner:     "avg:system.cpu.user{*}",
			expectedNesting:   1,
		},
		{
			name:              "query with nested default_zero",
			query:             "default_zero(default_zero(avg:system.cpu.user{*}))",
			expectDefaultZero: true,
			expectedInner:     "avg:system.cpu.user{*}",
			expectedNesting:   2,
		},
		{
			name:              "complex query with default_zero",
			query:             "default_zero(avg:rails.temporal.workflow_task.queue_time.avg{app:persona-web-temporal-worker-retention,env:production,region:us-central1,task_queue:retention}.fill(null))",
			expectDefaultZero: true,
			expectedInner:     "avg:rails.temporal.workflow_task.queue_time.avg{app:persona-web-temporal-worker-retention,env:production,region:us-central1,task_queue:retention}.fill(null)",
			expectedNesting:   1,
		},
		{
			name:              "query with spaces around default_zero",
			query:             "default_zero( avg:system.cpu.user{*} )",
			expectDefaultZero: true,
			expectedInner:     "avg:system.cpu.user{*}",
			expectedNesting:   1,
		},
		{
			name:              "query starting with default_zero but not function call",
			query:             "default_zero_custom_function(avg:system.cpu.user{*})",
			expectDefaultZero: false,
			expectedInner:     "",
			expectedNesting:   0,
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
