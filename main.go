package main

import (
	"context"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"retries-poc/consumer"
	"retries-poc/producer"
	"retries-poc/shared"
	"retries-poc/tracker"
)

// Connection Configuration
const (
	DefaultRabbitMQURL = "amqp://guest:guest@localhost:5672/"
)

// Timing Constants
const (
	RabbitMQWaitTime  = 5 * time.Second
	ConsumerStartWait = 2 * time.Second
	PollingInterval   = 1 * time.Second
	FinalLogsWait     = 1 * time.Second
	TimeoutDuration   = 2 * time.Minute
)

func main() {
	// Initialize random seed
	rand.Seed(time.Now().UnixNano())

	// Get RabbitMQ URL from environment
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = DefaultRabbitMQURL
	}

	// Create tracker
	msgTracker := tracker.New()

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for RabbitMQ to be ready
	log.Println("Waiting for RabbitMQ to be ready...")
	time.Sleep(RabbitMQWaitTime)

	// Create and start consumer
	c, err := consumer.New(rabbitURL, msgTracker)
	if err != nil {
		log.Fatalf("Failed to create consumer: %v", err)
	}
	defer c.Close()

	// Start consumer in background
	go func() {
		if err := c.Consume(ctx); err != nil {
			log.Printf("Consumer error: %v", err)
		}
	}()

	// Give consumer time to start
	time.Sleep(ConsumerStartWait)

	// Create producer
	p, err := producer.New(rabbitURL)
	if err != nil {
		log.Fatalf("Failed to create producer: %v", err)
	}
	defer p.Close()

	// Produce messages
	log.Println("Starting to produce messages...")
	if err := p.ProduceMessages(ctx, shared.DefaultMessageCount); err != nil {
		log.Fatalf("Failed to produce messages: %v", err)
	}

	log.Println("All messages sent. Waiting for processing...")

	// Wait for all messages to be processed or interrupt
	done := make(chan bool)
	go func() {
		for {
			if msgTracker.AllMessagesProcessed() {
				done <- true
				return
			}
			time.Sleep(PollingInterval)
		}
	}()

	select {
	case <-done:
		log.Println("All messages have been processed!")
	case <-sigChan:
		log.Println("Received interrupt signal")
	case <-time.After(TimeoutDuration):
		log.Println("Timeout: Not all messages were processed within timeout duration")
	}

	// Print the final report
	msgTracker.PrintReport()

	// Give a moment for any final logs
	time.Sleep(FinalLogsWait)
}