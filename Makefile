# Makefile for retries-poc development

.PHONY: help start-rabbitmq stop-rabbitmq run-app run-app-docker restart-app logs status install-air hot-reload

help: ## Show this help message
	@echo "Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

start-rabbitmq: ## Start RabbitMQ in background
	@echo "Starting RabbitMQ..."
	docker-compose up -d rabbitmq
	@echo "RabbitMQ started. Access management UI at http://localhost:15672 (guest/guest)"

stop-rabbitmq: ## Stop RabbitMQ
	@echo "Stopping RabbitMQ..."
	docker-compose down

run-app: ## Run app locally (fast development)
	@echo "Running app locally..."
	RABBITMQ_URL=amqp://guest:guest@localhost:5672/ go run main.go

run-app-docker: ## Run app in Docker
	@echo "Running app in Docker..."
	docker-compose --profile app up --build app

restart-app: ## Restart app in Docker
	@echo "Restarting app in Docker..."
	docker-compose --profile app down
	docker-compose --profile app up --build app

logs-rabbitmq: ## Show RabbitMQ logs
	docker-compose logs -f rabbitmq

logs-app: ## Show app logs
	docker-compose --profile app logs -f app

status: ## Show status of all services
	docker-compose ps

install-air: ## Install air for hot reload (one-time setup)
	go install github.com/air-verse/air@latest

hot-reload: ## Run app with hot reload (requires air to be installed)
	@echo "Starting hot reload development server..."
	@echo "Make sure RabbitMQ is running first: make start-rabbitmq"
	RABBITMQ_URL=amqp://guest:guest@localhost:5672/ air

# Default development workflow
dev: start-rabbitmq hot-reload ## Start RabbitMQ and run app with hot reload

# Clean up everything
clean: ## Stop all services and clean up
	docker-compose --profile app down
	docker-compose down
	docker system prune -f
