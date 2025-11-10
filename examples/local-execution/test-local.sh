#!/bin/bash
set -e

# Test script for local execution mode
# This script demonstrates how to use the local execution server

echo "=== Argo Workflows Local Execution Test ==="
echo ""

# Configuration
SERVER_URL="${SERVER_URL:-http://localhost:8080}"
WORKFLOW_FILE="${1:-hello-world.yaml}"

# Check if Docker is running
if ! docker ps > /dev/null 2>&1; then
    echo "Error: Docker is not running. Please start Docker and try again."
    exit 1
fi

# Check if server is running
echo "Checking if local execution server is running..."
if ! curl -s "${SERVER_URL}/healthz" > /dev/null; then
    echo "Error: Local execution server is not running at ${SERVER_URL}"
    echo "Please start the server with: argo local --port 8080"
    exit 1
fi

echo "✓ Server is healthy"
echo ""

# Submit workflow
echo "Submitting workflow: ${WORKFLOW_FILE}"
RESPONSE=$(curl -s -X POST "${SERVER_URL}/api/v1/workflows" \
    -H "Content-Type: application/yaml" \
    --data-binary "@${WORKFLOW_FILE}")

# Extract workflow name
WORKFLOW_NAME=$(echo "$RESPONSE" | grep -o '"name":"[^"]*"' | head -1 | cut -d'"' -f4)

if [ -z "$WORKFLOW_NAME" ]; then
    echo "Error: Failed to submit workflow"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "✓ Workflow submitted: ${WORKFLOW_NAME}"
echo ""

# Wait a bit for execution
echo "Waiting for workflow to execute..."
sleep 3

# Check workflow status
echo "Fetching workflow status..."
STATUS=$(curl -s "${SERVER_URL}/api/v1/workflows/${WORKFLOW_NAME}")

echo ""
echo "=== Workflow Status ==="
echo "$STATUS" | python3 -m json.tool 2>/dev/null || echo "$STATUS"
echo ""

# Extract phase
PHASE=$(echo "$STATUS" | grep -o '"phase":"[^"]*"' | cut -d'"' -f4)

if [ "$PHASE" = "Succeeded" ]; then
    echo "✓ Workflow completed successfully!"
    exit 0
elif [ "$PHASE" = "Failed" ]; then
    echo "✗ Workflow failed"
    exit 1
elif [ "$PHASE" = "Running" ]; then
    echo "⏳ Workflow is still running"
    exit 0
else
    echo "? Workflow status: ${PHASE}"
    exit 0
fi
