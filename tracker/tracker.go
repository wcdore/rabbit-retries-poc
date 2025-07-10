package tracker

import (
	"fmt"
	"sync"
	"time"

	"retries-poc/shared"
)

type MessageAttempt struct {
	Timestamp   time.Time
	Queue       string
	Result      string // "success" or "failure"
	TimeInQueue time.Duration
	RetryCount  int
}

type MessageJourney struct {
	MessageID   int
	Attempts    []MessageAttempt
	FinalStatus string
	TotalTime   time.Duration
	StartTime   time.Time
}

const (
	// Column widths for report formatting
	QueueJourneyColumnWidth = 54
)

type Tracker struct {
	journeys map[int]*MessageJourney
	mu       sync.RWMutex
}

func New() *Tracker {
	return &Tracker{
		journeys: make(map[int]*MessageJourney),
	}
}

func (t *Tracker) StartMessage(messageID int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.journeys[messageID] = &MessageJourney{
		MessageID: messageID,
		Attempts:  []MessageAttempt{},
		StartTime: time.Now(),
	}
}

func (t *Tracker) RecordAttempt(messageID int, queue string, result string, timeInQueue time.Duration, retryCount int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if journey, exists := t.journeys[messageID]; exists {
		attempt := MessageAttempt{
			Timestamp:   time.Now(),
			Queue:       queue,
			Result:      result,
			TimeInQueue: timeInQueue,
			RetryCount:  retryCount,
		}
		journey.Attempts = append(journey.Attempts, attempt)

		if result == "success" || retryCount >= shared.MaxRetries {
			journey.FinalStatus = result
			journey.TotalTime = time.Since(journey.StartTime)
		}
	}
}

func (t *Tracker) PrintReport() {
	t.mu.RLock()
	defer t.mu.RUnlock()

	fmt.Println("\n=== Message Journey Report ===")
	fmt.Printf("%-10s | %-8s | %-20s | %-*s | %-30s\n",
		"Message ID", "Attempts", "Final Status", QueueJourneyColumnWidth, "Queue Journey", "Time in Queues")
	// Match the separator line from the user's output
	fmt.Println("-----------------------------------------------------------------------------------------------------------------------------------------------------")

	// Iterate through messages in numerical order
	for i := 1; i <= shared.DefaultMessageCount; i++ {
		journey, exists := t.journeys[i]
		if !exists {
			continue
		}
		queueJourney := ""
		timeInQueues := ""

		// Build the queue journey
		for j, attempt := range journey.Attempts {
			if j > 0 {
				queueJourney += " → "
			}
			queueJourney += attempt.Queue
		}

		// If queueJourney is too long, truncate it
		runes := []rune(queueJourney)
		if len(runes) > QueueJourneyColumnWidth {
			queueJourney = string(runes[:QueueJourneyColumnWidth])
		}

		// Build time in queues for ALL attempts
		for j, attempt := range journey.Attempts {
			if j > 0 {
				timeInQueues += ", "
			}
			timeInQueues += fmt.Sprintf("%dms", attempt.TimeInQueue.Milliseconds())
		}

		finalStatus := journey.FinalStatus
		if finalStatus == "" {
			finalStatus = "In Progress"
		} else if finalStatus == "failure" && len(journey.Attempts) > shared.MaxRetries {
			finalStatus = "Failed (Max Retries)"
		} else if finalStatus == "success" {
			finalStatus = "Success"
		} else if finalStatus == "failure" {
			finalStatus = "Failed"
		}

		fmt.Printf("%-10d | %-8d | %-20s | %-*s | %-30s\n",
			journey.MessageID,
			len(journey.Attempts),
			finalStatus,
			QueueJourneyColumnWidth,
			queueJourney,
			timeInQueues)
	}

	fmt.Println("\n=== Summary ===")
	successCount := 0
	failureCount := 0
	totalAttempts := 0

	for _, journey := range t.journeys {
		totalAttempts += len(journey.Attempts)
		if journey.FinalStatus == "success" {
			successCount++
		} else if journey.FinalStatus == "failure" {
			failureCount++
		}
	}

	fmt.Printf("Total Messages: %d\n", len(t.journeys))
	fmt.Printf("Successful: %d\n", successCount)
	fmt.Printf("Failed: %d\n", failureCount)
	fmt.Printf("Total Attempts: %d\n", totalAttempts)
	fmt.Printf("Average Attempts per Message: %.2f\n", float64(totalAttempts)/float64(len(t.journeys)))
}

func (t *Tracker) AllMessagesProcessed() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.journeys) < shared.DefaultMessageCount {
		return false
	}

	for _, journey := range t.journeys {
		if journey.FinalStatus == "" {
			return false
		}
	}

	return true
}
