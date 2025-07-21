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
	// Exchanges
	ExchangeNameDelay = "delay_exchange"
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

	// Set up delay exchange
	err = c.channel.ExchangeDeclare(
		ExchangeNameDelay,        // name
		shared.ExchangeTypeTopic, // type
		true,                     // durable
		false,                    // auto-deleted
		false,                    // internal
		false,                    // no-wait
		nil,                      // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare delay exchange: %w", err)
	}

	// Set up retry exchange as headers exchange
	err = c.channel.ExchangeDeclare(
		ExchangeNameRetry,          // name
		shared.ExchangeTypeHeaders, // type - headers exchange for header-based routing
		true,                       // durable
		false,                      // auto-deleted
		false,                      // internal
		false,                      // no-wait
		nil,                        // arguments
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

	// Bind to retry exchange with header matching
	// Headers exchange will route based on original-queue header
	// Note: Headers exchange ignores 'x-' prefixed headers for routing
	headerMatch := amqp.Table{
		"original-queue": c.queueName,
		"x-match":        "all", // must match all specified headers
	}
	err = c.channel.QueueBind(
		c.queueName,       // queue name
		"",                // routing key is ignored for headers exchange
		ExchangeNameRetry, // exchange
		false,             // no-wait
		headerMatch,       // binding arguments for header matching
	)
	if err != nil {
		return fmt.Errorf("failed to bind queue to retry exchange: %w", err)
	}

	return nil
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

	// Debug logging for headers
	if retryCount > 0 {
		if origQueue, ok := delivery.Headers["original-queue"].(string); ok {
			log.Printf("[%s] Processing message %d from %s with retry count %d",
				c.queueName, msg.ID, origQueue, retryCount)
		}
	}

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
	// to determine which delay queue the message came from
	ttl := calculateTTL(retryCount - 1)
	return fmt.Sprintf("delay_%dms", ttl)
}

func (c *Consumer) simulateProcessing() bool {
	randomValue := rand.Float64()
	return randomValue <= c.successRate
}

func (c *Consumer) handleSuccess(msg shared.Message, delivery amqp.Delivery, queueName string, timeInQueue time.Duration, retryCount int) error {
	log.Printf("Message %d processed successfully (attempt %d of %d)", msg.ID, retryCount+1, shared.MaxRetries+1)
	c.tracker.RecordAttempt(msg.ID, abbreviatedQueueName(queueName), "success", timeInQueue, retryCount)
	return delivery.Ack(false)
}

// abbreviatedQueueName returns a simplified version of the queue name for reporting.
func abbreviatedQueueName(queueName string) string {
	// Check if this is a delay queue
	if strings.HasPrefix(queueName, "delay_") && strings.HasSuffix(queueName, "ms") {
		// For shared delay queues, just extract the delay
		// "delay_1000ms" -> "d-1000"
		delay := strings.TrimPrefix(queueName, "delay_")
		delay = strings.TrimSuffix(delay, "ms")
		return "d-" + delay
	}

	// Return non-delay queues as-is
	return queueName
}

func (c *Consumer) handleFailure(ctx context.Context, msg shared.Message, delivery amqp.Delivery, queueName string, timeInQueue time.Duration, retryCount int) error {
	log.Printf("Message %d failed (attempt %d of %d)", msg.ID, retryCount+1, shared.MaxRetries+1)
	c.tracker.RecordAttempt(msg.ID, abbreviatedQueueName(queueName), "failure", timeInQueue, retryCount)

	if retryCount >= shared.MaxRetries {
		log.Printf("Message %d has exhausted all %d retries, dropping message", msg.ID, shared.MaxRetries)
		return delivery.Ack(false) // Remove from queue
	}

	return c.scheduleRetry(ctx, msg, delivery, retryCount)
}

func (c *Consumer) scheduleRetry(ctx context.Context, msg shared.Message, delivery amqp.Delivery, retryCount int) error {
	ttl := calculateTTL(retryCount)
	delayQueueName := fmt.Sprintf("delay_%dms", ttl)

	// Create or ensure delay queue exists
	if err := c.createDelayQueue(ttl); err != nil {
		log.Printf("Failed to create delay queue: %v", err)
		return delivery.Nack(false, true)
	}

	// Update message timestamp for tracking time in next queue
	msg.Timestamp = time.Now()
	body, _ := json.Marshal(msg)

	// Publish to delay queue with updated retry count
	if err := c.publishToDelayQueue(ctx, body, ttl, retryCount+1); err != nil {
		log.Printf("Failed to publish to delay queue: %v", err)
		return delivery.Nack(false, true)
	}

	log.Printf("Message %d sent to %s (retry count: %d -> %d, TTL: %dms)", msg.ID, delayQueueName, retryCount, retryCount+1, ttl)
	return delivery.Ack(false)
}

func (c *Consumer) publishToDelayQueue(ctx context.Context, body []byte, ttl, newRetryCount int) error {
	return c.channel.PublishWithContext(
		ctx,
		ExchangeNameDelay,
		c.delayQueueRoutingKey(ttl),
		false,
		false,
		amqp.Publishing{
			ContentType: shared.JSONContentType,
			Body:        body,
			Headers: amqp.Table{
				HeaderRetryCount: int32(newRetryCount),
				"original-queue": c.queueName,
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

func (c *Consumer) createDelayQueue(ttl int) error {
	// Create shared delay queue name
	queueName := fmt.Sprintf("delay_%dms", ttl)

	args := amqp.Table{
		HeaderMessageTTL:           int32(ttl),
		HeaderDeadLetterExchange:   ExchangeNameRetry,
		HeaderDeadLetterRoutingKey: "retry.expired",                 // common routing key for all
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

	// Bind the delay queue to delay exchange
	err = c.channel.QueueBind(
		queueName,
		fmt.Sprintf("delay.%d", ttl), // simple TTL-based routing key
		ExchangeNameDelay,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to bind retry queue %s: %w", queueName, err)
	}

	return nil
}

func (c *Consumer) delayQueueRoutingKey(ttl int) string {
	return fmt.Sprintf("delay.%d", ttl)
}
