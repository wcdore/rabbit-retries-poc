package tracker

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// captureOutput captures stdout during function execution
func captureOutput(f func()) string {
	// Create a pipe to capture output
	r, w, _ := os.Pipe()

	// Save current stdout
	oldStdout := os.Stdout

	// Set stdout to our pipe
	os.Stdout = w

	// Execute the function
	f()

	// Restore stdout
	os.Stdout = oldStdout

	// Close writer and read output
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)

	return buf.String()
}

func TestTableOutput(t *testing.T) {
	tracker := New()

	// Set up test data with various scenarios
	testCases := []struct {
		messageID int
		attempts  []struct {
			queue  string
			result string
			delay  time.Duration
		}
	}{
		{
			// Simple success
			messageID: 1,
			attempts: []struct {
				queue  string
				result string
				delay  time.Duration
			}{
				{"order-ledger", "success", 10 * time.Millisecond},
			},
		},
		{
			// Multiple retries with success
			messageID: 2,
			attempts: []struct {
				queue  string
				result string
				delay  time.Duration
			}{
				{"order-ledger", "failure", 20 * time.Millisecond},
				{"ol-1000", "failure", 1005 * time.Millisecond},
				{"ol-2000", "success", 2003 * time.Millisecond},
			},
		},
		{
			// Max retries (5 attempts)
			messageID: 3,
			attempts: []struct {
				queue  string
				result string
				delay  time.Duration
			}{
				{"order-ledger", "failure", 30 * time.Millisecond},
				{"ol-1000", "failure", 1004 * time.Millisecond},
				{"ol-2000", "failure", 2002 * time.Millisecond},
				{"ol-4000", "failure", 4001 * time.Millisecond},
				{"ol-8000", "failure", 8000 * time.Millisecond},
			},
		},
		{
			// Journey that would be truncated
			messageID: 4,
			attempts: []struct {
				queue  string
				result string
				delay  time.Duration
			}{
				{"order-ledger", "failure", 40 * time.Millisecond},
				{"ol-1000", "failure", 1003 * time.Millisecond},
				{"ol-2000", "failure", 2001 * time.Millisecond},
				{"ol-4000", "failure", 4000 * time.Millisecond},
				{"ol-8000", "failure", 7999 * time.Millisecond},
				{"ol-16000", "success", 15998 * time.Millisecond},
			},
		},
		{
			// Another simple success
			messageID: 5,
			attempts: []struct {
				queue  string
				result string
				delay  time.Duration
			}{
				{"data-consumer", "success", 50 * time.Millisecond},
			},
		},
	}

	// Track messages
	for _, tc := range testCases {
		tracker.StartMessage(tc.messageID)
		for i, attempt := range tc.attempts {
			tracker.RecordAttempt(tc.messageID, attempt.queue, attempt.result, attempt.delay, i)
		}
	}

	// Capture actual output
	actualOutput := captureOutput(func() {
		tracker.PrintReport()
	})

	// Define expected output - this is what we expect the table to look like
	expectedOutput := `
=== Message Journey Report ===
Message ID | Attempts | Final Status         | Queue Journey                                          | Time in Queues
-----------------------------------------------------------------------------------------------------------------------------------------------------
1          | 1        | Success              | order-ledger                                           | 10ms
2          | 3        | Success              | order-ledger → ol-1000 → ol-2000                       | 20ms, 1005ms, 2003ms
3          | 5        | Failed (Max Retries) | order-ledger → ol-1000 → ol-2000 → ol-4000 → ol-8000   | 30ms, 1004ms, 2002ms, 4001ms, 8000ms
4          | 6        | Success              | order-ledger → ol-1000 → ol-2000 → ol-4000 → ol-8000 → | 40ms, 1003ms, 2001ms, 4000ms, 7999ms, 15998ms
5          | 1        | Success              | data-consumer                                          | 50ms

=== Summary ===
Total Messages: 5
Successful: 4
Failed: 1
Total Attempts: 16
Average Attempts per Message: 3.20
`

	// Normalize line endings and trim whitespace for comparison
	actualLines := strings.Split(strings.TrimSpace(actualOutput), "\n")
	expectedLines := strings.Split(strings.TrimSpace(expectedOutput), "\n")

	// Compare line by line for better error reporting
	if len(actualLines) != len(expectedLines) {
		t.Errorf("Output has %d lines, expected %d lines", len(actualLines), len(expectedLines))
		t.Logf("Actual output:\n%s", actualOutput)
		return
	}

	// First check if there are differences
	hasDifferences := false
	for i := range expectedLines {
		// Trim trailing spaces for comparison (they don't matter visually)
		actualLine := strings.TrimRight(actualLines[i], " ")
		expectedLine := strings.TrimRight(expectedLines[i], " ")

		if actualLine != expectedLine {
			t.Errorf("Line %d mismatch:\nExpected: '%s'\nActual:   '%s'",
				i+1, expectedLine, actualLine)
			hasDifferences = true
		}
	}

	// If there are differences, print the full actual output
	if hasDifferences {
		t.Logf("\nFull actual output:\n%s", actualOutput)
	}
}
