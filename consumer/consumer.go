package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"retries-poc/shared"
	"retries-poc/tracker"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Constants for configuration
const (
	// Exchange
	ExchangeNameRetry = "retry_exchange"

	// Consumer-specific retry configuration
	BaseRetryDelayMs   = 1000
	RetryQueueExpireMs = 10000 // How long the retry queue should exist after the last message is processed
)

// RabbitMQ Headers
const (
	HeaderRetryCount           = "x-retry-count"
	HeaderMessageTTL           = "x-message-ttl"
	HeaderDeadLetterExchange   = "x-dead-letter-exchange"
	HeaderDeadLetterRoutingKey = "x-dead-letter-routing-key"
	HeaderExpires              = "x-expires"
)

type Consumer struct {
	conn        *amqp.Connection
	channel     *amqp.Channel
	tracker     *tracker.Tracker
	queueName   string
	successRate float64
}

func New(url string, tracker *tracker.Tracker, queueName string, successRate float64) (*Consumer, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	consumer := &Consumer{
		conn:        conn,
		channel:     ch,
		tracker:     tracker,
		queueName:   queueName,
		successRate: successRate,
	}

	if err := consumer.setupInfrastructure(); err != nil {
		consumer.Close()
		return nil, err
	}

	return consumer, nil
}

func (c *Consumer) setupInfrastructure() error {
	if err := c.setupExchanges(); err != nil {
		return err
	}
	return c.setupWorkQueue()
}

func (c *Consumer) setupExchanges() error {
	// Set up working exchange as topic
	err := c.channel.ExchangeDeclare(
		shared.ExchangeNameTransaction, // name
		shared.ExchangeTypeTopic,       // type
		true,                           // durable
		false,                          // auto-deleted
		false,                          // internal
		false,                          // no-wait
		nil,                            // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare working exchange: %w", err)
	}

	// Set up retry exchange
	err = c.channel.ExchangeDeclare(
		ExchangeNameRetry,        // name
		shared.ExchangeTypeTopic, // type
		true,                     // durable
		false,                    // auto-deleted
		false,                    // internal
		false,                    // no-wait
		nil,                      // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare retry exchange: %w", err)
	}

	return nil
}

func (c *Consumer) setupWorkQueue() error {
	// Ensure queue exists
	_, err := c.channel.QueueDeclare(
		c.queueName, // name
		true,        // durable
		false,       // delete when unused
		false,       // exclusive
		false,       // no-wait
		nil,         // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue %s: %w", c.queueName, err)
	}

	// Bind queue to working exchange for all new transactions
	err = c.channel.QueueBind(
		c.queueName,                      // queue name
		shared.TopicTransactionProcessed, // routing key for new messages
		shared.ExchangeNameTransaction,   // exchange
		false,                            // no-wait
		nil,                              // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to bind queue to transaction.processed: %w", err)
	}

	// Also bind to queue-specific retry routing key
	err = c.channel.QueueBind(
		c.queueName,                    // queue name
		c.retryRoutingKey(),            // routing key for retries
		shared.ExchangeNameTransaction, // exchange
		false,                          // no-wait
		nil,                            // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to bind queue to retry routing key: %w", err)
	}

	return nil
}

func (c *Consumer) retryRoutingKey() string {
	return fmt.Sprintf("%s.%s", shared.TopicTransactionProcessed, c.queueName)
}

func (c *Consumer) Close() {
	if c.channel != nil {
		c.channel.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
}

func (c *Consumer) Consume(ctx context.Context) error {
	msgs, err := c.channel.Consume(
		c.queueName, // queue
		"",          // consumer
		false,       // auto-ack
		false,       // exclusive
		false,       // no-local
		false,       // no-wait
		nil,         // args
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	log.Printf("Consumer for %s started. Waiting for messages...", c.queueName)

	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-msgs:
			if !ok {
				return nil
			}

			if err := c.processMessage(ctx, d); err != nil {
				log.Printf("Error processing message: %v", err)
			}
		}
	}
}

func (c *Consumer) processMessage(ctx context.Context, delivery amqp.Delivery) error {
	var msg shared.Message
	if err := json.Unmarshal(delivery.Body, &msg); err != nil {
		log.Printf("Error unmarshaling message: %v", err)
		return delivery.Nack(false, false)
	}

	retryCount := getRetryCount(delivery.Headers)

	// Track the message if it's the first attempt
	if retryCount == 0 {
		c.tracker.StartMessage(msg.ID)
	}

	queueName := c.determineQueueName(retryCount)
	timeInQueue := time.Since(msg.Timestamp)

	if c.simulateProcessing() {
		return c.handleSuccess(msg, delivery, queueName, timeInQueue, retryCount)
	}

	return c.handleFailure(ctx, msg, delivery, queueName, timeInQueue, retryCount)
}

func getRetryCount(headers amqp.Table) int {
	if headers == nil {
		return 0
	}

	if rc, ok := headers[HeaderRetryCount]; ok {
		switch v := rc.(type) {
		case int32:
			return int(v)
		case int64:
			return int(v)
		case int:
			return v
		default:
			log.Printf("WARNING: Unexpected retry count type: %T (value: %v)", rc, rc)
		}
	}

	return 0
}

func (c *Consumer) determineQueueName(retryCount int) string {
	if retryCount == 0 {
		return c.queueName
	}

	// This should match the logic in scheduleRetry which uses the previous retry count
	// to determine which retry queue the message came from
	ttl := calculateTTL(retryCount - 1)
	return fmt.Sprintf("retry_%dms", ttl)
}

func (c *Consumer) simulateProcessing() bool {
	randomValue := rand.Float64()
	return randomValue <= c.successRate
}

func (c *Consumer) handleSuccess(msg shared.Message, delivery amqp.Delivery, queueName string, timeInQueue time.Duration, retryCount int) error {
	log.Printf("Message %d processed successfully after %d retries", msg.ID, retryCount)
	c.tracker.RecordAttempt(msg.ID, abbreviatedQueueName(queueName), "success", timeInQueue, retryCount)
	return delivery.Ack(false)
}

// abbreviatedQueueName returns a simplified version of the queue name for reporting.
func abbreviatedQueueName(queueName string) string {
	// Check if this is a retry queue
	if strings.HasPrefix(queueName, "retry_") && strings.HasSuffix(queueName, "ms") {
		// For shared retry queues, just extract the delay
		// "retry_1000ms" -> "retry-1000"
		delay := strings.TrimPrefix(queueName, "retry_")
		delay = strings.TrimSuffix(delay, "ms")
		return "r-" + delay
	}

	// Return non-retry queues as-is
	return queueName
}

func (c *Consumer) handleFailure(ctx context.Context, msg shared.Message, delivery amqp.Delivery, queueName string, timeInQueue time.Duration, retryCount int) error {
	log.Printf("Message %d failed, retry count: %d", msg.ID, retryCount)
	c.tracker.RecordAttempt(msg.ID, abbreviatedQueueName(queueName), "failure", timeInQueue, retryCount)

	if retryCount >= shared.MaxRetries {
		log.Printf("Message %d exceeded max retries (%d), marking as failed", msg.ID, shared.MaxRetries)
		return delivery.Ack(false) // Remove from queue
	}

	return c.scheduleRetry(ctx, msg, delivery, retryCount)
}

func (c *Consumer) scheduleRetry(ctx context.Context, msg shared.Message, delivery amqp.Delivery, retryCount int) error {
	ttl := calculateTTL(retryCount)
	retryQueueName := fmt.Sprintf("retry_%dms", ttl)

	// Create or ensure retry queue exists
	if err := c.createRetryQueue(ttl); err != nil {
		log.Printf("Failed to create retry queue: %v", err)
		return delivery.Nack(false, true)
	}

	// Update message timestamp for tracking time in next queue
	msg.Timestamp = time.Now()
	body, _ := json.Marshal(msg)

	// Publish to retry queue with updated retry count
	if err := c.publishToRetryQueue(ctx, body, ttl, retryCount+1); err != nil {
		log.Printf("Failed to publish to retry queue: %v", err)
		return delivery.Nack(false, true)
	}

	log.Printf("Message %d sent to %s (retry count: %d -> %d, TTL: %dms)", msg.ID, retryQueueName, retryCount, retryCount+1, ttl)
	return delivery.Ack(false)
}

func (c *Consumer) publishToRetryQueue(ctx context.Context, body []byte, ttl, newRetryCount int) error {
	return c.channel.PublishWithContext(
		ctx,
		ExchangeNameRetry,
		c.retryQueueRoutingKey(ttl),
		false,
		false,
		amqp.Publishing{
			ContentType: shared.JSONContentType,
			Body:        body,
			Headers: amqp.Table{
				HeaderRetryCount: int32(newRetryCount),
			},
		},
	)
}

func calculateTTL(retryCount int) int {
	// Exponential backoff: 1000ms, 2000ms, 4000ms, 8000ms, 16000ms
	ttl := BaseRetryDelayMs
	for i := 0; i < retryCount; i++ {
		ttl *= 2
	}
	return ttl
}

func (c *Consumer) createRetryQueue(ttl int) error {
	// Create shared retry queue name (no consumer prefix)
	queueName := fmt.Sprintf("retry_%dms", ttl)

	args := amqp.Table{
		HeaderMessageTTL:           int32(ttl),
		HeaderDeadLetterExchange:   shared.ExchangeNameTransaction,
		HeaderDeadLetterRoutingKey: c.retryRoutingKey(),
		HeaderExpires:              int32(ttl + RetryQueueExpireMs), // Queue expires 10s after TTL
	}

	_, err := c.channel.QueueDeclare(
		queueName,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		args,
	)
	if err != nil {
		return fmt.Errorf("failed to declare retry queue %s: %w", queueName, err)
	}

	// Bind the retry queue to retry exchange with wildcard pattern to accept all consumers
	// This allows shared retry queues to receive messages from any consumer
	wildcardRoutingKey := fmt.Sprintf("retry.%d.*", ttl)
	err = c.channel.QueueBind(
		queueName,
		wildcardRoutingKey,
		ExchangeNameRetry,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to bind retry queue %s: %w", queueName, err)
	}

	return nil
}

func (c *Consumer) retryQueueRoutingKey(ttl int) string {
	return fmt.Sprintf("retry.%d.%s", ttl, c.queueName)
}
