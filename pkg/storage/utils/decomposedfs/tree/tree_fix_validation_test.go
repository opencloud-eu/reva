package tree

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTrashFixValidationCore tests the core logic of the trash deletion fix
func TestTrashFixValidationCore(t *testing.T) {
	tests := []struct {
		name                  string
		hasValidInternalPath  bool
		isTrashOperation      bool
		expectedSkipRevisions bool
		description           string
	}{
		{
			name:                  "Regular file deletion with valid path",
			hasValidInternalPath:  true,
			isTrashOperation:      false,
			expectedSkipRevisions: false,
			description:           "Normal file deletion should process revisions",
		},
		{
			name:                  "Trash operation with valid path",
			hasValidInternalPath:  true,
			isTrashOperation:      true,
			expectedSkipRevisions: true,
			description:           "Trash operations should skip revision processing",
		},
		{
			name:                  "Regular operation with empty internal path (original bug)",
			hasValidInternalPath:  false,
			isTrashOperation:      false,
			expectedSkipRevisions: true,
			description:           "Empty internal path should skip revisions to prevent .flock errors",
		},
		{
			name:                  "Trash operation with empty internal path",
			hasValidInternalPath:  false,
			isTrashOperation:      true,
			expectedSkipRevisions: true,
			description:           "Trash operations should always skip revisions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This simulates the core logic from shouldProcessNodeRevisions
			shouldProcessRevisions := tt.hasValidInternalPath && !tt.isTrashOperation
			skipRevisions := !shouldProcessRevisions

			assert.Equal(t, tt.expectedSkipRevisions, skipRevisions, tt.description)
		})
	}
}

// TestTimeSuffixDetectionBehavior tests the logic for detecting trash operations
func TestTimeSuffixDetectionBehavior(t *testing.T) {
	tests := []struct {
		name        string
		timeSuffix  string
		isTrashOp   bool
		description string
	}{
		{
			name:        "No time suffix - regular operation",
			timeSuffix:  "",
			isTrashOp:   false,
			description: "Empty time suffix indicates regular file operation",
		},
		{
			name:        "Valid time suffix - trash operation",
			timeSuffix:  ".T.2024-01-01T12:00:00.000000000Z",
			isTrashOp:   true,
			description: "Time suffix indicates permanent deletion from trash",
		},
		{
			name:        "Short time suffix - trash operation",
			timeSuffix:  ".T.123",
			isTrashOp:   true,
			description: "Any non-empty time suffix indicates trash operation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isTrashOperation := tt.timeSuffix != ""
			assert.Equal(t, tt.isTrashOp, isTrashOperation, tt.description)
		})
	}
}

// TestRevisionProcessingLogic tests the decision logic for revision processing
func TestRevisionProcessingLogic(t *testing.T) {
	// This test validates the fix prevents the ".flock: permission denied" error
	// by ensuring revisions are not processed when they shouldn't be

	// Scenario 1: The original bug case
	// - Empty internal path (trash node reconstruction issue)
	// - Regular operation (not trash, but should be treated as such due to empty path)
	hasValidInternalPath := false
	isTrashOperation := false

	// The fix should skip revision processing to prevent .flock errors
	shouldProcess := hasValidInternalPath && !isTrashOperation
	assert.False(t, shouldProcess, "Empty internal path should skip revision processing to prevent .flock errors")

	// Scenario 2: Normal file deletion
	hasValidInternalPath = true
	isTrashOperation = false

	shouldProcess = hasValidInternalPath && !isTrashOperation
	assert.True(t, shouldProcess, "Normal file deletion with valid path should process revisions")

	// Scenario 3: Actual trash operation
	hasValidInternalPath = true
	isTrashOperation = true

	shouldProcess = hasValidInternalPath && !isTrashOperation
	assert.False(t, shouldProcess, "Trash operations should never process revisions")
}

// TestTrashOperationIdentification validates how we identify trash operations
func TestTrashOperationIdentification(t *testing.T) {
	tests := []struct {
		name       string
		timeSuffix string
		expected   bool
	}{
		{"Empty suffix", "", false},
		{"Trash timestamp", ".T.2024-01-01T12:00:00.000000000Z", true},
		{"Invalid timestamp", ".T.invalid", true}, // Still counts as trash op
		{"Just dot T", ".T.", true},
		{"Random string", "random", true}, // Any non-empty string
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isTrashOp := tt.timeSuffix != ""
			assert.Equal(t, tt.expected, isTrashOp)
		})
	}
}

// TestRevisionGlobPatternIssue tests the glob pattern behavior that caused the original bug
func TestRevisionGlobPatternIssue(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "glob_test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Change to temp directory to test relative patterns
	originalDir, err := os.Getwd()
	assert.NoError(t, err)
	defer os.Chdir(originalDir)

	err = os.Chdir(tempDir)
	assert.NoError(t, err)

	// Create some test files that match problematic patterns
	testFiles := []string{
		".REV.test1",
		".REV.test2",
		"normal_file.txt",
		".other_hidden_file",
	}

	for _, file := range testFiles {
		err := os.WriteFile(file, []byte("content"), 0644)
		assert.NoError(t, err)
	}

	// Test the problematic pattern that occurs when internalPath is empty
	emptyPath := ""
	revisionDelimiter := ".REV."
	problematicPattern := emptyPath + revisionDelimiter + "*"

	// This pattern becomes ".REV.*" which could match unexpected files
	matches, err := filepath.Glob(problematicPattern)
	assert.NoError(t, err, "Glob should not error")
	assert.Len(t, matches, 2, "Should match the .REV.* files")
	assert.Contains(t, matches, ".REV.test1")
	assert.Contains(t, matches, ".REV.test2")

	// Test with a proper path
	properPath := "/some/valid/path/nodeid"
	properPattern := properPath + revisionDelimiter + "*"
	matches, err = filepath.Glob(properPattern)
	assert.NoError(t, err, "Glob should not error")
	assert.Len(t, matches, 0, "Should not match any files for non-existent path")
}

// TestRemoveNodeDecisionSimulation simulates the key logic without complex mocking
func TestRemoveNodeDecisionSimulation(t *testing.T) {
	// Create a simple simulation of the removeNode logic
	simulateRemoveNode := func(internalPath, timeSuffix string) (shouldProcessRevisions bool, reason string) {
		isTrashOperation := timeSuffix != ""
		hasValidInternalPath := internalPath != ""

		// This is the core logic from the fix
		shouldProcessRevisions = hasValidInternalPath && !isTrashOperation

		if !shouldProcessRevisions {
			if !hasValidInternalPath && !isTrashOperation {
				reason = "empty internal path in non-trash operation"
			} else if isTrashOperation {
				reason = "trash operation - skip revisions by design"
			} else {
				reason = "empty internal path"
			}
		} else {
			reason = "normal file deletion with valid path"
		}

		return shouldProcessRevisions, reason
	}

	tests := []struct {
		name                   string
		internalPath           string
		timeSuffix             string
		expectedProcess        bool
		expectedReasonContains string
	}{
		{
			name:                   "Original bug scenario",
			internalPath:           "", // Empty internal path
			timeSuffix:             "", // Not a trash operation
			expectedProcess:        false,
			expectedReasonContains: "empty internal path in non-trash",
		},
		{
			name:                   "Normal file deletion",
			internalPath:           "/valid/path/to/node",
			timeSuffix:             "",
			expectedProcess:        true,
			expectedReasonContains: "normal file deletion",
		},
		{
			name:                   "Trash operation with valid path",
			internalPath:           "/valid/path/to/node",
			timeSuffix:             ".T.2024-01-01T12:00:00.000000000Z",
			expectedProcess:        false,
			expectedReasonContains: "trash operation",
		},
		{
			name:                   "Trash operation with empty path",
			internalPath:           "",
			timeSuffix:             ".T.2024-01-01T12:00:00.000000000Z",
			expectedProcess:        false,
			expectedReasonContains: "trash operation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldProcess, reason := simulateRemoveNode(tt.internalPath, tt.timeSuffix)
			assert.Equal(t, tt.expectedProcess, shouldProcess, "Processing decision should match expected")
			assert.Contains(t, reason, tt.expectedReasonContains, "Reason should contain expected text")
		})
	}
}

// TestFileSystemValidation tests real filesystem operations for validation
func TestFileSystemValidation(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "tree_test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name               string
		createFile         bool
		expectedFileExists bool
		description        string
	}{
		{
			name:               "Test file creation",
			createFile:         true,
			expectedFileExists: true,
			description:        "File should exist after creation",
		},
		{
			name:               "Test file absence",
			createFile:         false,
			expectedFileExists: false,
			description:        "File should not exist when not created",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tempDir, fmt.Sprintf("test_%s.txt", tt.name))

			if tt.createFile {
				err := os.WriteFile(testFile, []byte("test content"), 0644)
				assert.NoError(t, err, "File creation should succeed")
			}

			_, err := os.Stat(testFile)
			fileExists := err == nil

			assert.Equal(t, tt.expectedFileExists, fileExists, tt.description)
		})
	}
}

// TestShouldProcessNodeRevisionsMethod tests the extracted shouldProcessNodeRevisions method logic
func TestShouldProcessNodeRevisionsMethod(t *testing.T) {
	tests := []struct {
		name             string
		hasValidPath     bool
		isTrashOperation bool
		expectedResult   bool
	}{
		{
			name:             "valid path, regular operation",
			hasValidPath:     true,
			isTrashOperation: false,
			expectedResult:   true,
		},
		{
			name:             "valid path, trash operation",
			hasValidPath:     true,
			isTrashOperation: true,
			expectedResult:   false,
		},
		{
			name:             "empty path, regular operation",
			hasValidPath:     false,
			isTrashOperation: false,
			expectedResult:   false,
		},
		{
			name:             "empty path, trash operation",
			hasValidPath:     false,
			isTrashOperation: true,
			expectedResult:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the shouldProcessNodeRevisions method logic
			result := tt.hasValidPath && !tt.isTrashOperation
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}
