package tus

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"golang.org/x/exp/slog"
)

// logTestFunction simulates a function that logs via slog
func logTestFunction(logger *slog.Logger) {
	logger.Info("test message from logTestFunction") // Line 16
}

// anotherLogFunction is another test function
func anotherLogFunction(logger *slog.Logger) {
	logger.Warn("warning from anotherLogFunction") // Line 21
}

// debugLogFunction tests debug level logging
func debugLogFunction(logger *slog.Logger) {
	logger.Debug("debug from debugLogFunction") // Line 26
}

// errorLogFunction tests error level logging
func errorLogFunction(logger *slog.Logger) {
	logger.Error("error from errorLogFunction") // Line 31
}

func TestTusdLoggerCallerTracking(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Create zerolog logger that writes to buffer
	zlog := zerolog.New(&buf).With().Timestamp().Logger()

	// Create slog logger with our tusdLogger handler
	slogger := slog.New(tusdLogger{log: &zlog})

	// Test 1: INFO level - should have line info
	logTestFunction(slogger)

	// Parse the first log entry
	var logEntry1 map[string]interface{}
	lines := strings.Split(buf.String(), "\n")
	if err := json.Unmarshal([]byte(lines[0]), &logEntry1); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// Check that the line field exists (INFO should have it)
	line1, ok := logEntry1["line"].(string)
	if !ok {
		t.Fatal("Expected 'line' field in INFO level log entry")
	}

	// Verify it contains the test file name and approximate line number
	if !strings.Contains(line1, "tus_test.go") {
		t.Errorf("Expected line to contain 'tus_test.go', got: %s", line1)
	}

	// Check line number is around 17 (allowing for small variations)
	if !strings.Contains(line1, ":17") && !strings.Contains(line1, ":16") && !strings.Contains(line1, ":18") {
		t.Errorf("Expected line number around 17, got: %s", line1)
	}

	// Check message
	if msg, ok := logEntry1["message"].(string); !ok || msg != "test message from logTestFunction" {
		t.Errorf("Expected message 'test message from logTestFunction', got: %v", logEntry1["message"])
	}

	// Check level
	if level, ok := logEntry1["level"].(string); !ok || level != "info" {
		t.Errorf("Expected level 'info', got: %v", logEntry1["level"])
	}

	// Clear buffer for next test
	buf.Reset()

	// Test 2: WARN level - should NOT have line info
	anotherLogFunction(slogger)

	// Parse the second log entry
	var logEntry2 map[string]interface{}
	lines = strings.Split(buf.String(), "\n")
	if err := json.Unmarshal([]byte(lines[0]), &logEntry2); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// WARN level should NOT have line field (only DEBUG and INFO get line tracking)
	if _, ok := logEntry2["line"]; ok {
		t.Error("Expected NO 'line' field in WARN level log entry")
	}

	// Check message
	if msg, ok := logEntry2["message"].(string); !ok || msg != "warning from anotherLogFunction" {
		t.Errorf("Expected message 'warning from anotherLogFunction', got: %v", logEntry2["message"])
	}

	// Check level
	if level, ok := logEntry2["level"].(string); !ok || level != "warn" {
		t.Errorf("Expected level 'warn', got: %v", logEntry2["level"])
	}

	// Clear buffer for next test
	buf.Reset()

	// Test 3: DEBUG level - should have line info
	debugLogFunction(slogger)

	var logEntry3 map[string]interface{}
	lines = strings.Split(buf.String(), "\n")
	if err := json.Unmarshal([]byte(lines[0]), &logEntry3); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// Check that the line field exists (DEBUG should have it)
	line3, ok := logEntry3["line"].(string)
	if !ok {
		t.Fatal("Expected 'line' field in DEBUG level log entry")
	}

	// Verify it contains the test file name and approximate line number
	if !strings.Contains(line3, "tus_test.go") {
		t.Errorf("Expected line to contain 'tus_test.go', got: %s", line3)
	}

	// Check line number is around 26
	if !strings.Contains(line3, ":26") && !strings.Contains(line3, ":25") && !strings.Contains(line3, ":27") {
		t.Errorf("Expected line number around 26, got: %s", line3)
	}

	// Clear buffer for next test
	buf.Reset()

	// Test 4: ERROR level - should NOT have line info
	errorLogFunction(slogger)

	var logEntry4 map[string]interface{}
	lines = strings.Split(buf.String(), "\n")
	if err := json.Unmarshal([]byte(lines[0]), &logEntry4); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// ERROR level should NOT have line field
	if _, ok := logEntry4["line"]; ok {
		t.Error("Expected NO 'line' field in ERROR level log entry")
	}

	// Verify that DEBUG and INFO have different line numbers
	if line1 == line3 {
		t.Errorf("Expected different line numbers for INFO and DEBUG, but both were: %s", line1)
	}
}

func TestTusdLoggerPathNotTrimmed(t *testing.T) {
	var buf bytes.Buffer
	zlog := zerolog.New(&buf).With().Timestamp().Logger()
	slogger := slog.New(tusdLogger{log: &zlog})

	// Log a message at INFO level (which should have line info)
	slogger.Info("test path not trimmed")

	// Parse log entry
	var logEntry map[string]interface{}
	lines := strings.Split(buf.String(), "\n")
	if err := json.Unmarshal([]byte(lines[0]), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	line, ok := logEntry["line"].(string)
	if !ok {
		t.Fatal("Expected 'line' field in log entry")
	}

	// The path should be the FULL path (not trimmed)
	// It should contain the filename
	if !strings.Contains(line, "tus_test.go") {
		t.Errorf("Expected line to contain 'tus_test.go', got: %s", line)
	}

	// Log the full path for inspection
	t.Logf("Full path logged: %s", line)
}

func TestTusdLoggerWithAttributes(t *testing.T) {
	var buf bytes.Buffer
	zlog := zerolog.New(&buf).With().Timestamp().Logger()
	handler := tusdLogger{log: &zlog}

	// Create logger with attributes using WithAttrs
	handlerWithAttrs := handler.WithAttrs([]slog.Attr{
		slog.String("uploadId", "abc123"),
		slog.Int64("size", 1024),
		slog.Bool("completed", true),
	})

	slogger := slog.New(handlerWithAttrs)

	// Log a message
	slogger.Info("upload complete")

	// Parse log entry
	var logEntry map[string]interface{}
	lines := strings.Split(buf.String(), "\n")
	if err := json.Unmarshal([]byte(lines[0]), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// Check that attributes were added
	if uploadId, ok := logEntry["uploadId"].(string); !ok || uploadId != "abc123" {
		t.Errorf("Expected uploadId 'abc123', got: %v", logEntry["uploadId"])
	}

	if size, ok := logEntry["size"].(float64); !ok || size != 1024 {
		t.Errorf("Expected size 1024, got: %v", logEntry["size"])
	}

	if completed, ok := logEntry["completed"].(bool); !ok || !completed {
		t.Errorf("Expected completed true, got: %v", logEntry["completed"])
	}
}

func TestTusdLoggerLevels(t *testing.T) {
	tests := []struct {
		name           string
		logFunc        func(*slog.Logger)
		expectedLevel  string
		shouldHaveLine bool
	}{
		{
			name:           "debug level",
			logFunc:        func(l *slog.Logger) { l.Debug("debug message") },
			expectedLevel:  "debug",
			shouldHaveLine: true,
		},
		{
			name:           "info level",
			logFunc:        func(l *slog.Logger) { l.Info("info message") },
			expectedLevel:  "info",
			shouldHaveLine: true,
		},
		{
			name:           "warn level",
			logFunc:        func(l *slog.Logger) { l.Warn("warn message") },
			expectedLevel:  "warn",
			shouldHaveLine: false,
		},
		{
			name:           "error level",
			logFunc:        func(l *slog.Logger) { l.Error("error message") },
			expectedLevel:  "error",
			shouldHaveLine: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			zlog := zerolog.New(&buf).With().Timestamp().Logger()
			slogger := slog.New(tusdLogger{log: &zlog})

			tt.logFunc(slogger)

			var logEntry map[string]interface{}
			lines := strings.Split(buf.String(), "\n")
			if err := json.Unmarshal([]byte(lines[0]), &logEntry); err != nil {
				t.Fatalf("Failed to parse log entry: %v", err)
			}

			if level, ok := logEntry["level"].(string); !ok || level != tt.expectedLevel {
				t.Errorf("Expected level %s, got: %v", tt.expectedLevel, logEntry["level"])
			}

			// Check if line field presence matches expectation
			_, hasLine := logEntry["line"]
			if tt.shouldHaveLine && !hasLine {
				t.Errorf("Expected 'line' field for %s level, but it was missing", tt.expectedLevel)
			}
			if !tt.shouldHaveLine && hasLine {
				t.Errorf("Expected NO 'line' field for %s level, but it was present", tt.expectedLevel)
			}
		})
	}
}

func TestTusdLoggerEnabled(t *testing.T) {
	var buf bytes.Buffer
	zlog := zerolog.New(&buf)
	handler := tusdLogger{log: &zlog}

	// Enabled should always return true
	if !handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Expected Enabled to return true for Debug")
	}
	if !handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Expected Enabled to return true for Info")
	}
	if !handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("Expected Enabled to return true for Warn")
	}
	if !handler.Enabled(context.Background(), slog.LevelError) {
		t.Error("Expected Enabled to return true for Error")
	}
}

func TestTusdLoggerRecordWithInlineAttributes(t *testing.T) {
	var buf bytes.Buffer
	zlog := zerolog.New(&buf).With().Timestamp().Logger()
	slogger := slog.New(tusdLogger{log: &zlog})

	// Log with inline attributes
	slogger.Info("processing file",
		slog.String("filename", "test.txt"),
		slog.Int64("bytes", 2048),
	)

	var logEntry map[string]interface{}
	lines := strings.Split(buf.String(), "\n")
	if err := json.Unmarshal([]byte(lines[0]), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// Verify inline attributes are present
	if filename, ok := logEntry["filename"].(string); !ok || filename != "test.txt" {
		t.Errorf("Expected filename 'test.txt', got: %v", logEntry["filename"])
	}

	if bytes, ok := logEntry["bytes"].(float64); !ok || bytes != 2048 {
		t.Errorf("Expected bytes 2048, got: %v", logEntry["bytes"])
	}
}
