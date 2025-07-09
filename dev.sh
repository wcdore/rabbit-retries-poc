#!/bin/bash

# Development script for retries-poc

case "$1" in
    "start-rabbitmq")
        echo "Starting RabbitMQ..."
        docker-compose up -d rabbitmq
        echo "RabbitMQ started. Access management UI at http://localhost:15672 (guest/guest)"
        ;;
    "stop-rabbitmq")
        echo "Stopping RabbitMQ..."
        docker-compose down
        ;;
    "run-app")
        echo "Running app locally (make sure RabbitMQ is running)..."
        RABBITMQ_URL=amqp://guest:guest@localhost:5672/ go run main.go
        ;;
    "run-app-docker")
        echo "Running app in Docker..."
        docker-compose --profile app up --build app
        ;;
    "restart-app")
        echo "Restarting app in Docker..."
        docker-compose --profile app down
        docker-compose --profile app up --build app
        ;;
    "logs")
        if [ "$2" = "rabbitmq" ]; then
            docker-compose logs -f rabbitmq
        elif [ "$2" = "app" ]; then
            docker-compose --profile app logs -f app
        else
            echo "Usage: $0 logs [rabbitmq|app]"
        fi
        ;;
    "status")
        docker-compose ps
        ;;
    *)
        echo "Usage: $0 {start-rabbitmq|stop-rabbitmq|run-app|run-app-docker|restart-app|logs [rabbitmq|app]|status}"
        echo ""
        echo "Development workflow:"
        echo "1. $0 start-rabbitmq    # Start RabbitMQ in background"
        echo "2. $0 run-app          # Run app locally (recommended for development)"
        echo "   OR"
        echo "   $0 run-app-docker   # Run app in Docker"
        echo ""
        echo "For quick development iterations:"
        echo "- Use '$0 run-app' to run locally with hot reload"
        echo "- Use '$0 restart-app' to rebuild and restart the Docker app"
        ;;
esac
