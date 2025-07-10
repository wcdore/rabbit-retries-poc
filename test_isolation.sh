#!/bin/bash
# Test script to verify retry isolation

echo "Starting RabbitMQ..."
docker-compose up -d rabbitmq

echo "Waiting for RabbitMQ to be ready..."
sleep 10

echo "Running the POC..."
RABBITMQ_URL=amqp://guest:guest@localhost:5672/ go run main.go

echo "Test complete!"