# RabbitMQ Exponential Backoff Retry POC

This POC demonstrates the RabbitMQ exponential backoff retry pattern described in [this article](https://www.brianstorti.com/rabbitmq-exponential-backoff/).

## Key Features

- Dynamic retry queue creation based on message TTL
- Automatic queue deletion when empty (10 seconds after last message)
- Exponential backoff: 1s, 2s, 4s, 8s
- Random 2/3 failure rate for realistic testing
- Comprehensive message journey tracking
- Final report showing each message's path through the system

## Architecture

- **Working Exchange**: Direct exchange for normal message flow
- **Retry Exchange**: Topic exchange for retry routing
- **Work Queue**: Main queue for message processing
- **Dynamic Retry Queues**: Created on-demand with pattern `retry_queue_{ttl}ms`

## Prerequisites

- Docker and Docker Compose installed
- Go 1.20+ (if running locally without Docker)
- You'll need go 1.23 if you want to run air for hot reloading.

## Development Workflow

For fast development iterations, you can keep RabbitMQ running while making changes to the app code:

### Option 1: Using Makefile (Recommended)

```bash
# One-time setup for hot reload (optional)
make install-air

# Start development (starts RabbitMQ + hot reload)
make dev

# Or step by step:
make start-rabbitmq   # Start RabbitMQ in background
make hot-reload       # Run app with hot reload (requires air)
# OR
make run-app          # Run app locally without hot reload

# Other useful commands:
make help            # Show all available commands
make status          # Show service status
make logs-rabbitmq   # View RabbitMQ logs
make stop-rabbitmq   # Stop RabbitMQ
make clean           # Clean up everything
```

### Option 2: Using the dev script

```bash
# Start RabbitMQ in background
./dev.sh start-rabbitmq

# Run app locally for fast development
./dev.sh run-app

# Or run app in Docker
./dev.sh run-app-docker

# Restart app in Docker after changes
./dev.sh restart-app

# View help
./dev.sh
```

### Option 3: Manual Docker commands

```bash
# Start only RabbitMQ
docker-compose up -d rabbitmq

# Run app locally (fastest for development)
RABBITMQ_URL=amqp://guest:guest@localhost:5672/ go run main.go

# Or run app in Docker
docker-compose --profile app up --build app

# Stop only the app (keep RabbitMQ running)
docker-compose --profile app down

# Stop everything
docker-compose down
```

## Running the POC

### Using Docker Compose (Full System)

```bash
# Clone or navigate to the project directory
cd retries-poc

# Start the system
docker-compose up --build

# The POC will automatically:
# 1. Start RabbitMQ with management interface (http://localhost:15672)
# 2. Wait for RabbitMQ to be ready
# 3. Send 20 numbered messages
# 4. Process with 2/3 random failure rate
# 5. Retry failed messages with exponential backoff
# 6. Display a final report table
```

### Running Locally

```bash
# Start RabbitMQ (using Docker)
docker run -d --name rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3-management

# Install dependencies
go mod download

# Run the POC
RABBITMQ_URL=amqp://guest:guest@localhost:5672/ go run main.go
```

## Message Flow

1. Producer sends messages 1-20 to `work_queue`
2. Consumer processes each message with random 66% failure rate
3. Failed messages are published to retry exchange with incremental TTL
4. Dynamic retry queues are created for each unique TTL
5. After TTL expires, messages are dead-lettered back to `work_queue`
6. Process repeats up to 5 times per message

## Output Example

```
=== Message Journey Report ===

Message ID | Attempts | Final Status  | Queue Journey                                              | Time in Queues
-----------|----------|---------------|------------------------------------------------------------|--------------------------
1          | 3        | Success       | work_queue → retry_queue_1000ms → retry_queue_2000ms → work_queue | 0ms, 1000ms, 2000ms, 0ms
2          | 1        | Success       | work_queue                                                 | 0ms
3          | 6        | Failed        | work_queue → retry_queue_1000ms → ... → retry_queue_16000ms | 0ms, 1000ms, ..., 16000ms
...

=== Summary ===
Total Messages: 20
Successful: 14
Failed: 6
Total Attempts: 67
Average Attempts per Message: 3.35
```

## Configuration

The POC uses the following configuration:
- **Max Retries**: 5 attempts per message
- **Failure Rate**: 66% (random > 0.33)
- **Exponential Backoff**: 1s × 2^(retry_count)
- **Queue Auto-Delete**: TTL + 10 seconds

## RabbitMQ Management

Access the RabbitMQ management interface at http://localhost:15672
- Username: guest
- Password: guest

## Clean Up

```bash
# Remove containers and volumes
docker-compose down -v

# Or if running locally
docker stop rabbitmq
docker rm rabbitmq
```

## Project Structure

```
retries-poc/
├── consumer/         # Message consumer with retry logic
├── producer/         # Message producer
├── tracker/          # Message journey tracking
├── main.go          # Main application entry point
├── go.mod           # Go module definition
├── go.sum           # Dependency checksums
├── docker-compose.yml
├── Dockerfile
└── README.md
```
