package main

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"sync"
	stdtemplate "text/template"
	"time"

	"github.com/hako/durafmt"
	alerttemplate "github.com/prometheus/alertmanager/template"
)

// AlertTemplateConfig defines the template configuration for alert messages
type AlertTemplateConfig struct {
	Title          string          `json:"title"`          // Message title template (optional)
	Color          string          `json:"color"`          // Color template (optional, defaults to status-based color)
	FieldsTemplate []FieldTemplate `json:"fieldsTemplate"` // Field templates
}

// FieldTemplate defines a single field in the attachment
type FieldTemplate struct {
	Title string `json:"title"` // Field title template
	Value string `json:"value"` // Field value template
	Short bool   `json:"short"` // Whether this field should be short
}

// TemplateData is the data exposed to templates
type TemplateData struct {
	Alert       alerttemplate.Alert // Current alert
	ExternalURL string              // Alertmanager URL
	Receiver    string              // Receiver name
	Status      string              // Overall message status (firing/resolved)
	ReceivedAt  time.Time           // Time when webhook was received
	ConfigID    string              // AlertManager Config ID
}

// KeyValue represents a key-value pair for sorted map iteration
type KeyValue struct {
	Key   string
	Value string
}

// TemplateResult contains the rendered template output
type TemplateResult struct {
	Title  string
	Color  string
	Fields []FieldResult
}

// FieldResult contains the rendered field output
type FieldResult struct {
	Title string
	Value string
	Short bool
}

// templateFuncMap returns the function map available in templates
func templateFuncMap() stdtemplate.FuncMap {
	return stdtemplate.FuncMap{
		"title":       titleFunc,
		"upper":       strings.ToUpper,
		"lower":       strings.ToLower,
		"formatTime":  formatTimeFunc,
		"duration":    durationFunc,
		"color":       colorFunc,
		"sortByLabel": sortByLabelFunc,
		"statusEmoji": statusEmojiFunc,
		"labels":      labelsFunc,
		"annotations": annotationsFunc,
	}
}

// titleFunc capitalizes the first letter of each word
func titleFunc(s string) string {
	if s == "" {
		return s
	}
	// Simple title case - capitalize first character
	result := strings.ToUpper(s[:1]) + s[1:]
	return result
}

// formatTimeFunc formats time in RFC1123 format
func formatTimeFunc(t time.Time) string {
	return t.Format(time.RFC1123)
}

// durationFunc calculates the duration between start time and reference time
// If only one argument, uses current time as reference
func durationFunc(start time.Time, args ...time.Time) string {
	var end time.Time
	if len(args) > 0 {
		end = args[0]
	} else {
		end = time.Now()
	}

	if start.IsZero() || end.IsZero() {
		return ""
	}
	d := end.Sub(start)
	if d < 0 {
		d = -d
	}
	return durafmt.Parse(d).LimitFirstN(2).String()
}

// colorFunc returns the color for a status
func colorFunc(status string) string {
	switch strings.ToLower(status) {
	case "firing":
		return colorFiring
	case "resolved":
		return colorResolved
	default:
		return colorExpired
	}
}

// sortByLabelFunc sorts a map by keys and returns key-value pairs
func sortByLabelFunc(m map[string]string) []KeyValue {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	result := make([]KeyValue, len(keys))
	for i, k := range keys {
		result[i] = KeyValue{Key: k, Value: m[k]}
	}
	return result
}

// statusEmojiFunc returns the status message with emoji
func statusEmojiFunc(status string) string {
	switch strings.ToLower(status) {
	case "firing":
		return ":fire: FIRING :fire:"
	case "resolved":
		return "RESOLVED"
	default:
		return strings.ToUpper(status)
	}
}

// labelsFunc returns sorted labels as formatted string
func labelsFunc(m map[string]string) string {
	kvs := sortByLabelFunc(m)
	var result string
	for _, kv := range kvs {
		result = fmt.Sprintf("%s**%s:** %s\n", result, titleFunc(kv.Key), kv.Value)
	}
	return result
}

// annotationsFunc returns sorted annotations as formatted string
func annotationsFunc(m map[string]string) string {
	return labelsFunc(m) // Same format
}

// TemplateRenderer handles template parsing and execution with caching
type TemplateRenderer struct {
	cache map[string]*stdtemplate.Template
	mu    sync.RWMutex
}

// NewTemplateRenderer creates a new template renderer
func NewTemplateRenderer() *TemplateRenderer {
	return &TemplateRenderer{
		cache: make(map[string]*stdtemplate.Template),
	}
}

// parseTemplate parses a template string and caches the result
func (tr *TemplateRenderer) parseTemplate(name, text string) (*stdtemplate.Template, error) {
	cacheKey := name + ":" + text

	tr.mu.RLock()
	cached, ok := tr.cache[cacheKey]
	tr.mu.RUnlock()

	if ok {
		return cached, nil
	}

	tmpl, err := stdtemplate.New(name).Funcs(templateFuncMap()).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	tr.mu.Lock()
	tr.cache[cacheKey] = tmpl
	tr.mu.Unlock()

	return tmpl, nil
}

// RenderAlert renders an alert using the given template configuration
func (tr *TemplateRenderer) RenderAlert(tmplConfig *AlertTemplateConfig, data *TemplateData) (*TemplateResult, error) {
	if tmplConfig == nil {
		return nil, fmt.Errorf("template config is required")
	}

	result := &TemplateResult{}

	// Render title
	if tmplConfig.Title != "" {
		tmpl, err := tr.parseTemplate("title", tmplConfig.Title)
		if err != nil {
			return nil, fmt.Errorf("failed to parse title template: %w", err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("failed to execute title template: %w", err)
		}
		result.Title = buf.String()
	}

	// Render color
	if tmplConfig.Color != "" {
		tmpl, err := tr.parseTemplate("color", tmplConfig.Color)
		if err != nil {
			return nil, fmt.Errorf("failed to parse color template: %w", err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("failed to execute color template: %w", err)
		}
		result.Color = buf.String()
	}

	// Render fields
	result.Fields = make([]FieldResult, len(tmplConfig.FieldsTemplate))
	for i, fieldTmpl := range tmplConfig.FieldsTemplate {
		fieldResult := FieldResult{Short: fieldTmpl.Short}

		// Render field title
		if fieldTmpl.Title != "" {
			tmpl, err := tr.parseTemplate("field_title", fieldTmpl.Title)
			if err != nil {
				return nil, fmt.Errorf("failed to parse field title template: %w", err)
			}
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, data); err != nil {
				return nil, fmt.Errorf("failed to execute field title template: %w", err)
			}
			fieldResult.Title = buf.String()
		}

		// Render field value
		if fieldTmpl.Value != "" {
			tmpl, err := tr.parseTemplate("field_value", fieldTmpl.Value)
			if err != nil {
				return nil, fmt.Errorf("failed to parse field value template: %w", err)
			}
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, data); err != nil {
				return nil, fmt.Errorf("failed to execute field value template: %w", err)
			}
			fieldResult.Value = buf.String()
		}

		result.Fields[i] = fieldResult
	}

	return result, nil
}

// ValidateTemplate validates a template configuration
func (tr *TemplateRenderer) ValidateTemplate(tmplConfig *AlertTemplateConfig) error {
	// Test with sample data
	testData := &TemplateData{
		Alert: alerttemplate.Alert{
			Status:       "firing",
			Labels:       map[string]string{"alertname": "TestAlert", "severity": "critical"},
			Annotations:  map[string]string{"summary": "Test summary", "description": "Test description"},
			StartsAt:     time.Now().Add(-time.Hour),
			EndsAt:       time.Now(),
			GeneratorURL: "http://prometheus.example.com/graph",
		},
		ExternalURL: "http://alertmanager.example.com",
		Receiver:    "test-receiver",
		Status:      "firing",
		ReceivedAt:  time.Now(),
		ConfigID:    "test-config",
	}

	_, err := tr.RenderAlert(tmplConfig, testData)
	return err
}