package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"

	"retries-poc/shared"
	"retries-poc/tracker"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Constants for configuration
const (
	// Exchange types
	RetryExchange     = "retry_exchange"
	ExchangeTypeRetry = "topic"

	// Consumer-specific retry configuration
	BaseRetryDelayMs   = 1000
	RetryQueueExpireMs = 10000
	SuccessRate        = 0.33
)

// RabbitMQ Headers
const (
	RetryCountHeader     = "x-retry-count"
	MessageTTLHeader     = "x-message-ttl"
	DeadLetterExchange   = "x-dead-letter-exchange"
	DeadLetterRoutingKey = "x-dead-letter-routing-key"
	ExpiresHeader        = "x-expires"
)

type Consumer struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	tracker *tracker.Tracker
}

func New(url string, tracker *tracker.Tracker) (*Consumer, error) {
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
		conn:    conn,
		channel: ch,
		tracker: tracker,
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
	// Set up working exchange
	err := c.channel.ExchangeDeclare(
		shared.WorkingExchange,    // name
		shared.ExchangeTypeDirect, // type
		true,                      // durable
		false,                     // auto-deleted
		false,                     // internal
		false,                     // no-wait
		nil,                       // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare working exchange: %w", err)
	}

	// Set up retry exchange
	err = c.channel.ExchangeDeclare(
		RetryExchange,     // name
		ExchangeTypeRetry, // type
		true,              // durable
		false,             // auto-deleted
		false,             // internal
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare retry exchange: %w", err)
	}

	return nil
}

func (c *Consumer) setupWorkQueue() error {
	// Ensure work queue exists
	_, err := c.channel.QueueDeclare(
		shared.WorkQueue, // name
		true,             // durable
		false,            // delete when unused
		false,            // exclusive
		false,            // no-wait
		nil,              // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare work queue: %w", err)
	}

	// Bind work queue to working exchange
	err = c.channel.QueueBind(
		shared.WorkQueue,       // queue name
		shared.WorkRoutingKey,  // routing key
		shared.WorkingExchange, // exchange
		false,                  // no-wait
		nil,                    // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to bind work queue: %w", err)
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
		shared.WorkQueue, // queue
		"",               // consumer
		false,            // auto-ack
		false,            // exclusive
		false,            // no-local
		false,            // no-wait
		nil,              // args
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	log.Println("Consumer started. Waiting for messages...")

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

	queueName := determineQueueName(retryCount)
	timeInQueue := time.Since(msg.Timestamp)

	if simulateProcessing() {
		return c.handleSuccess(msg, delivery, queueName, timeInQueue, retryCount)
	}

	return c.handleFailure(ctx, msg, delivery, queueName, timeInQueue, retryCount)
}

func getRetryCount(headers amqp.Table) int {
	if headers == nil {
		return 0
	}

	if rc, ok := headers[RetryCountHeader]; ok {
		if count, ok := rc.(int32); ok {
			return int(count)
		}
	}

	return 0
}

func determineQueueName(retryCount int) string {
	if retryCount == 0 {
		return shared.WorkQueue
	}

	ttl := calculateTTL(retryCount - 1)
	return fmt.Sprintf("retry_queue_%dms", ttl)
}

func simulateProcessing() bool {
	randomValue := rand.Float64()
	return randomValue <= SuccessRate
}

func (c *Consumer) handleSuccess(msg shared.Message, delivery amqp.Delivery, queueName string, timeInQueue time.Duration, retryCount int) error {
	log.Printf("Message %d processed successfully after %d retries", msg.ID, retryCount)
	c.tracker.RecordAttempt(msg.ID, queueName, "success", timeInQueue, retryCount)
	return delivery.Ack(false)
}

func (c *Consumer) handleFailure(ctx context.Context, msg shared.Message, delivery amqp.Delivery, queueName string, timeInQueue time.Duration, retryCount int) error {
	log.Printf("Message %d failed, retry count: %d", msg.ID, retryCount)
	c.tracker.RecordAttempt(msg.ID, queueName, "failure", timeInQueue, retryCount)

	if retryCount >= shared.MaxRetries {
		log.Printf("Message %d exceeded max retries (%d), marking as failed", msg.ID, shared.MaxRetries)
		return delivery.Ack(false) // Remove from queue
	}

	return c.scheduleRetry(ctx, msg, delivery, retryCount)
}

func (c *Consumer) scheduleRetry(ctx context.Context, msg shared.Message, delivery amqp.Delivery, retryCount int) error {
	ttl := calculateTTL(retryCount)

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

	log.Printf("Message %d sent to retry_queue_%dms", msg.ID, ttl)
	return delivery.Ack(false)
}

func (c *Consumer) publishToRetryQueue(ctx context.Context, body []byte, ttl, newRetryCount int) error {
	return c.channel.PublishWithContext(
		ctx,
		RetryExchange,
		fmt.Sprintf("retry.%d", ttl),
		false,
		false,
		amqp.Publishing{
			ContentType: shared.JSONContentType,
			Body:        body,
			Headers: amqp.Table{
				RetryCountHeader: int32(newRetryCount),
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
	queueName := fmt.Sprintf("retry_queue_%dms", ttl)

	args := amqp.Table{
		MessageTTLHeader:     int32(ttl),
		DeadLetterExchange:   shared.WorkingExchange,
		DeadLetterRoutingKey: shared.WorkRoutingKey,
		ExpiresHeader:        int32(ttl + RetryQueueExpireMs), // Expire 10s after TTL
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

	// Bind the retry queue to retry exchange
	err = c.channel.QueueBind(
		queueName,
		fmt.Sprintf("retry.%d", ttl),
		RetryExchange,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to bind retry queue %s: %w", queueName, err)
	}

	return nil
}
