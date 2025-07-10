package producer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"retries-poc/shared"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Producer struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

func New(url string) (*Producer, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	return &Producer{
		conn:    conn,
		channel: ch,
	}, nil
}

func (p *Producer) Close() {
	if p.channel != nil {
		p.channel.Close()
	}
	if p.conn != nil {
		p.conn.Close()
	}
}

func (p *Producer) ProduceMessages(ctx context.Context, count int) error {
	// Declare the working exchange as topic type
	err := p.channel.ExchangeDeclare(
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

	// Send messages
	for i := 1; i <= count; i++ {
		msg := shared.Message{
			ID:        i,
			Content:   fmt.Sprintf("Test message %d", i),
			Timestamp: time.Now(),
		}

		body, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}

		err = p.channel.PublishWithContext(
			ctx,
			shared.ExchangeNameTransaction,   // exchange
			shared.TopicTransactionProcessed, // routing key
			false,                            // mandatory
			false,                            // immediate
			amqp.Publishing{
				ContentType: shared.JSONContentType,
				Body:        body,
			},
		)
		if err != nil {
			return fmt.Errorf("failed to publish message: %w", err)
		}

		log.Printf("Sent message %d", i)
	}

	return nil
}
