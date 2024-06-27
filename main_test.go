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

		expectedErr := "open tests/datadogmetric-no-file.yaml: no such file or directory"
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

// TODO: figure out how to mock calls to datadog so we don't need to use our API keys in the tests.
func TestMetricFetching(t *testing.T) {
	t.SkipNow()
}
