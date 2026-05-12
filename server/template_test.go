package main

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/alertmanager/template"
)

func TestTemplateFuncMap(t *testing.T) {
	tests := []struct {
		name     string
		funcName string
		input    interface{}
		args     []interface{}
		expected string
	}{
		{
			name:     "title empty string",
			funcName: "title",
			input:    "",
			expected: "",
		},
		{
			name:     "title lowercase",
			funcName: "title",
			input:    "hello",
			expected: "Hello",
		},
		{
			name:     "title already uppercase",
			funcName: "title",
			input:    "Hello",
			expected: "Hello",
		},
		{
			name:     "title single char",
			funcName: "title",
			input:    "a",
			expected: "A",
		},
		{
			name:     "upper lowercase",
			funcName: "upper",
			input:    "hello",
			expected: "HELLO",
		},
		{
			name:     "lower uppercase",
			funcName: "lower",
			input:    "HELLO",
			expected: "hello",
		},
		{
			name:     "formatTime valid",
			funcName: "formatTime",
			input:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			expected: "Mon, 15 Jan 2024 10:30:00 UTC",
		},
		{
			name:     "duration with reference time",
			funcName: "duration",
			input:    time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC),
			args:     []interface{}{time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)},
			expected: "1 hour",
		},
		{
			name:     "color firing",
			funcName: "color",
			input:    "firing",
			expected: "#FF0000",
		},
		{
			name:     "color resolved",
			funcName: "color",
			input:    "resolved",
			expected: "#008000",
		},
		{
			name:     "color unknown",
			funcName: "color",
			input:    "unknown",
			expected: "#F0F8FF",
		},
		{
			name:     "statusEmoji firing",
			funcName: "statusEmoji",
			input:    "firing",
			expected: ":fire: FIRING :fire:",
		},
		{
			name:     "statusEmoji resolved",
			funcName: "statusEmoji",
			input:    "resolved",
			expected: "RESOLVED",
		},
		{
			name:     "statusEmoji unknown",
			funcName: "statusEmoji",
			input:    "pending",
			expected: "PENDING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := templateFuncMap()
			fn, ok := fm[tt.funcName]
			if !ok {
				t.Fatalf("function %s not found in FuncMap", tt.funcName)
			}

			// Call function based on type
			var result string
			switch tt.funcName {
			case "title":
				result = fn.(func(string) string)(tt.input.(string))
			case "upper":
				result = fn.(func(string) string)(tt.input.(string))
			case "lower":
				result = fn.(func(string) string)(tt.input.(string))
			case "formatTime":
				result = fn.(func(time.Time) string)(tt.input.(time.Time))
			case "duration":
				if len(tt.args) > 0 {
					result = fn.(func(time.Time, ...time.Time) string)(tt.input.(time.Time), tt.args[0].(time.Time))
				} else {
					result = fn.(func(time.Time, ...time.Time) string)(tt.input.(time.Time))
				}
			case "color":
				result = fn.(func(string) string)(tt.input.(string))
			case "statusEmoji":
				result = fn.(func(string) string)(tt.input.(string))
			}

			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSortByLabelFunc(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected []KeyValue
	}{
		{
			name:     "nil map",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty map",
			input:    map[string]string{},
			expected: []KeyValue{},
		},
		{
			name:  "single item",
			input: map[string]string{"a": "1"},
			expected: []KeyValue{
				{Key: "a", Value: "1"},
			},
		},
		{
			name:  "multiple items sorted",
			input: map[string]string{"c": "3", "a": "1", "b": "2"},
			expected: []KeyValue{
				{Key: "a", Value: "1"},
				{Key: "b", Value: "2"},
				{Key: "c", Value: "3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sortByLabelFunc(tt.input)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d items, got %d", len(tt.expected), len(result))
				return
			}

			for i, kv := range result {
				if kv.Key != tt.expected[i].Key || kv.Value != tt.expected[i].Value {
					t.Errorf("item %d: expected {%s, %s}, got {%s, %s}",
						i, tt.expected[i].Key, tt.expected[i].Value, kv.Key, kv.Value)
				}
			}
		})
	}
}

func TestRenderAlert(t *testing.T) {
	renderer := NewTemplateRenderer()

	testData := &TemplateData{
		Alert: template.Alert{
			Status:       "firing",
			Labels:       map[string]string{"alertname": "TestAlert", "severity": "critical"},
			Annotations:  map[string]string{"summary": "Test summary"},
			StartsAt:     time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC),
			EndsAt:       time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			GeneratorURL: "http://prometheus.example.com/graph",
		},
		ExternalURL: "http://alertmanager.example.com",
		Receiver:    "test-receiver",
		Status:      "firing",
		ReceivedAt:  time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		ConfigID:    "test-config",
	}

	tests := []struct {
		name        string
		tmplConfig  *AlertTemplateConfig
		expectError bool
		checkResult func(t *testing.T, result *TemplateResult)
	}{
		{
			name:       "nil config",
			tmplConfig: nil,
			expectError: true,
		},
		{
			name: "simple title",
			tmplConfig: &AlertTemplateConfig{
				Title: "{{ .Alert.Status | upper }}",
			},
			checkResult: func(t *testing.T, result *TemplateResult) {
				if result.Title != "FIRING" {
					t.Errorf("expected title 'FIRING', got %q", result.Title)
				}
			},
		},
		{
			name: "color template",
			tmplConfig: &AlertTemplateConfig{
				Color: "{{ color .Status }}",
			},
			checkResult: func(t *testing.T, result *TemplateResult) {
				if result.Color != "#FF0000" {
					t.Errorf("expected color '#FF0000', got %q", result.Color)
				}
			},
		},
		{
			name: "field with labels",
			tmplConfig: &AlertTemplateConfig{
				FieldsTemplate: []FieldTemplate{
					{
						Title: "Labels",
						Value: "{{ range $kv := .Alert.Labels | sortByLabel }}**{{ $kv.Key }}:** {{ $kv.Value }}\n{{ end }}",
						Short: true,
					},
				},
			},
			checkResult: func(t *testing.T, result *TemplateResult) {
				if len(result.Fields) != 1 {
					t.Errorf("expected 1 field, got %d", len(result.Fields))
					return
				}
				if result.Fields[0].Title != "Labels" {
					t.Errorf("expected field title 'Labels', got %q", result.Fields[0].Title)
				}
				if !strings.Contains(result.Fields[0].Value, "alertname") {
					t.Errorf("expected field value to contain 'alertname', got %q", result.Fields[0].Value)
				}
			},
		},
		{
			name: "invalid template syntax",
			tmplConfig: &AlertTemplateConfig{
				Title: "{{ .InvalidSyntax",
			},
			expectError: true,
		},
		{
			name: "invalid field template",
			tmplConfig: &AlertTemplateConfig{
				FieldsTemplate: []FieldTemplate{
					{
						Value: "{{ .InvalidFieldSyntax",
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := renderer.RenderAlert(tt.tmplConfig, testData)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.checkResult != nil {
				tt.checkResult(t, result)
			}
		})
	}
}

func TestValidateTemplate(t *testing.T) {
	renderer := NewTemplateRenderer()

	tests := []struct {
		name        string
		tmplConfig  *AlertTemplateConfig
		expectError bool
	}{
		{
			name: "valid empty template",
			tmplConfig: &AlertTemplateConfig{},
			expectError: false,
		},
		{
			name: "valid title template",
			tmplConfig: &AlertTemplateConfig{
				Title: "Alert: {{ .Alert.Labels.alertname }}",
			},
			expectError: false,
		},
		{
			name: "valid color template",
			tmplConfig: &AlertTemplateConfig{
				Color: "{{ if eq .Status \"firing\" }}#FF0000{{ else }}#00FF00{{ end }}",
			},
			expectError: false,
		},
		{
			name: "valid fields template",
			tmplConfig: &AlertTemplateConfig{
				FieldsTemplate: []FieldTemplate{
					{
						Title: "{{ .Alert.Labels.alertname }}",
						Value: "{{ .Alert.Annotations.summary }}",
						Short: true,
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid title syntax",
			tmplConfig: &AlertTemplateConfig{
				Title: "{{ .Invalid",
			},
			expectError: true,
		},
		{
			name: "invalid color syntax",
			tmplConfig: &AlertTemplateConfig{
				Color: "{{ .Invalid",
			},
			expectError: true,
		},
		{
			name: "invalid field syntax",
			tmplConfig: &AlertTemplateConfig{
				FieldsTemplate: []FieldTemplate{
					{
						Value: "{{ .Invalid",
					},
				},
			},
			expectError: true,
		},
		{
			name: "template with non-existent field",
			tmplConfig: &AlertTemplateConfig{
				Title: "{{ .NonExistent.Field }}",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := renderer.ValidateTemplate(tt.tmplConfig)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestTemplateCaching(t *testing.T) {
	renderer := NewTemplateRenderer()

	// Parse same template twice
	tmpl1, err := renderer.parseTemplate("test", "{{ .Alert.Status }}")
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	tmpl2, err := renderer.parseTemplate("test", "{{ .Alert.Status }}")
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}

	// Verify cache works (same template object)
	if tmpl1 != tmpl2 {
		t.Errorf("expected same template object from cache, got different objects")
	}

	// Parse different template
	tmpl3, err := renderer.parseTemplate("test", "{{ .Alert.Labels.alertname }}")
	if err != nil {
		t.Fatalf("third parse failed: %v", err)
	}

	// Should be different object
	if tmpl1 == tmpl3 {
		t.Errorf("expected different template object for different content")
	}
}

func TestAnnotationsFunc(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected string
	}{
		{
			name:     "empty map",
			input:    map[string]string{},
			expected: "",
		},
		{
			name:     "single annotation",
			input:    map[string]string{"summary": "test summary"},
			expected: "**Summary:** test summary\n",
		},
		{
			name:     "multiple annotations sorted",
			input:    map[string]string{"description": "test desc", "summary": "test sum"},
			expected: "**Description:** test desc\n**Summary:** test sum\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := annotationsFunc(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestLabelsAndAnnotationsFunc(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected string
	}{
		{
			name:     "empty map",
			input:    map[string]string{},
			expected: "",
		},
		{
			name:     "single item",
			input:    map[string]string{"key": "value"},
			expected: "**Key:** value\n",
		},
		{
			name:     "multiple items sorted",
			input:    map[string]string{"b": "2", "a": "1"},
			expected: "**A:** 1\n**B:** 2\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := labelsFunc(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestDurationFuncEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
		expected string
	}{
		{
			name:     "zero start time",
			start:    time.Time{},
			end:      time.Now(),
			expected: "",
		},
		{
			name:     "zero end time",
			start:    time.Now(),
			end:      time.Time{},
			expected: "",
		},
		{
			name:     "negative duration",
			start:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC),
			expected: "1 hour", // Should handle negative by flipping
		},
		{
			name:     "very short duration",
			start:    time.Now().Add(-time.Second),
			end:      time.Now(),
			expected: "1 second",
		},
		{
			name:     "complex duration",
			start:    time.Now().Add(-time.Hour - 30*time.Minute),
			end:      time.Now(),
			expected: "1 hour 30 minutes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := durationFunc(tt.start, tt.end)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}