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

## Multiple Consumer Pattern and Retry Isolation

When implementing this retry pattern with multiple consumers, there's a critical challenge: preventing retry messages from being delivered to unintended queues. If not handled properly, when a message from one queue is retried, it could be delivered to ALL queues bound to the same routing key, causing duplicate processing.

### The Problem

Without proper isolation:

1. Producer publishes to `transaction.processed`
2. Both `order-ledger` and `data-consumer` queues receive the message
3. When `order-ledger` fails and retries a message, the retry could go to BOTH queues
4. `data-consumer` would receive duplicate messages on each retry attempt

### Solution Options

#### Option 1: Direct Queue-to-Queue Dead Lettering (Not Used)

- Configure dead-letter messages to go directly to specific queues
- **Pros**: Most precise control
- **Cons**: Tighter coupling, less flexible

#### Option 2: Shared Retry Queues with Routing Key Patterns (Current Implementation)

- Shared retry queues used by all consumers (e.g., `retry_1000ms`, `retry_2000ms`)
- Routing keys follow pattern: `retry.{ttl}.{queue}` (e.g., `retry.1000.order-ledger`)
- Retry queues bind to wildcard pattern: `retry.{ttl}.*`
- Dead letter routing remains queue-specific to ensure proper return path
- **Pros**: Reduced infrastructure, proper isolation via routing, efficient resource usage
- **Cons**: Slightly more complex routing logic

#### Option 3: Message Headers for Queue Targeting (Not Used)

- Add headers indicating the intended queue
- Use header-based routing
- **Pros**: Flexible, supports dynamic queue addition
- **Cons**: More complex exchange configuration, requires producer awareness

### Current Implementation Details

This POC uses **Option 2** with the following approach:

1. **Initial Message Flow**:
   - Producer publishes to `TRANSACTION` exchange with routing key `transaction.processed`
   - Both queues (`order-ledger` and `data-consumer`) receive all messages

2. **Shared Retry Infrastructure**:
   - Shared retry queues: `retry_1000ms`, `retry_2000ms`, `retry_4000ms`, `retry_8000ms`
   - Routing keys follow pattern: `retry.{ttl}.{queue}`
     - `order-ledger` uses: `retry.1000.order-ledger`, `retry.2000.order-ledger`, etc.
     - `data-consumer` uses: `retry.1000.data-consumer`, `retry.2000.data-consumer`, etc.
   - Each retry queue binds to wildcard pattern: `retry.1000.*`, `retry.2000.*`, etc.
   - Dead-letter routing keys remain queue-specific: `transaction.processed.order-ledger`

3. **Benefits**:
   - Reduced infrastructure (N retry queues instead of N×M where M is number of consumers)
   - Complete retry isolation between consumers via routing
   - No duplicate messages during retries
   - Efficient resource utilization
   - Easy to add new consumers without creating new retry queues

## RabbitMQ Management

Access the RabbitMQ management interface at http://localhost:15672
- Username: guest
- Password: guest

## Testing

The project includes comprehensive tests for the table formatting logic:

```bash
# Run all tests
./test.sh

# Run specific test suites
go test ./tracker -v                           # All tracker tests
go test ./tracker -run TestQueueJourneyTruncation -v  # Test truncation logic
go test ./tracker -run TestTableAlignment -v          # Test column alignment
go test ./tracker -run TestQueueJourneyColumnWidth -v # Test width constraints
go test ./tracker -run TestRealWorldScenarios -v      # Test realistic scenarios

# Run tests with coverage
go test ./tracker -cover -v
```

The tests verify:
- Queue journey truncation at appropriate boundaries
- Consistent column alignment across all rows
- Proper handling of the QueueJourneyColumnWidth constant
- Real-world retry scenarios with various queue lengths

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
