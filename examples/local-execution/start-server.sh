#!/bin/bash

# Start the Argo local execution server with proper Docker configuration

# Detect Docker socket location
if [ -S "/var/run/docker.sock" ]; then
    # Docker Desktop on Linux or standard Docker
    export DOCKER_HOST="unix:///var/run/docker.sock"
elif [ -S "$HOME/.colima/default/docker.sock" ]; then
    # Colima (common on macOS)
    export DOCKER_HOST="unix://$HOME/.colima/default/docker.sock"
elif [ -S "$HOME/.docker/run/docker.sock" ]; then
    # Docker Desktop on macOS (newer versions)
    export DOCKER_HOST="unix://$HOME/.docker/run/docker.sock"
else
    echo "⚠️  Warning: Could not find Docker socket"
    echo "Please ensure Docker is running and set DOCKER_HOST manually if needed"
    echo ""
    echo "For Colima users, run:"
    echo "  export DOCKER_HOST=unix://\$HOME/.colima/default/docker.sock"
    echo ""
fi

# Display Docker configuration
echo "Docker Configuration:"
echo "  DOCKER_HOST: ${DOCKER_HOST:-<not set, using default>}"
echo ""

# Test Docker connection
if docker ps > /dev/null 2>&1; then
    echo "✓ Docker is accessible"
    echo ""
else
    echo "✗ Cannot connect to Docker"
    echo "Please ensure Docker is running"
    exit 1
fi

# Start the server
cd "$(dirname "$0")/../.."
echo "Starting Argo local execution server..."
./dist/argo local --port 8080
