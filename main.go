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

	// Create separate trackers for each queue
	orderLedgerTracker := tracker.New()
	dataConsumerTracker := tracker.New()

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for RabbitMQ to be ready
	log.Println("Waiting for RabbitMQ to be ready...")
	time.Sleep(RabbitMQWaitTime)

	// Create and start order-ledger consumer (33% success rate)
	orderLedgerConsumer, err := consumer.New(rabbitURL, orderLedgerTracker, shared.QueueOrderLedger, 0.33)
	if err != nil {
		log.Fatalf("Failed to create order-ledger consumer: %v", err)
	}
	defer orderLedgerConsumer.Close()

	// Create and start data-consumer (100% success rate)
	dataConsumer, err := consumer.New(rabbitURL, dataConsumerTracker, shared.QueueDataConsumer, 1.0)
	if err != nil {
		log.Fatalf("Failed to create data-consumer: %v", err)
	}
	defer dataConsumer.Close()

	// Start both consumers in background
	go func() {
		if err := orderLedgerConsumer.Consume(ctx); err != nil {
			log.Printf("Order-ledger consumer error: %v", err)
		}
	}()

	go func() {
		if err := dataConsumer.Consume(ctx); err != nil {
			log.Printf("Data-consumer error: %v", err)
		}
	}()

	// Give consumers time to start
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
			// Check if both trackers have processed all messages
			if orderLedgerTracker.AllMessagesProcessed() && dataConsumerTracker.AllMessagesProcessed() {
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

	// Print both reports
	log.Println("\n=== Order Ledger Queue Message Journey Report ===")
	orderLedgerTracker.PrintReport()

	log.Println("\n=== Data Consumer Queue Message Journey Report ===")
	dataConsumerTracker.PrintReport()

	// Give a moment for any final logs
	time.Sleep(FinalLogsWait)
}
