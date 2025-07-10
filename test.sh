#!/bin/bash
# Test runner script for retries-poc

echo "Running tracker table formatting test with coverage..."
echo "====================================================="

# Run the comprehensive table output test with coverage
go test -v -cover ./tracker -run TestTableOutput