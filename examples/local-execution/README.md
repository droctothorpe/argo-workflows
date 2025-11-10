# Argo Workflows Local Execution

This directory contains examples for running Argo Workflows locally using Docker containers instead of Kubernetes.

## Overview

The local execution mode allows you to develop and test workflows on your local machine without needing a Kubernetes cluster. Workflows are executed as ephemeral Docker containers, making it easy to iterate quickly during development.

## Prerequisites

- Docker installed and running on your machine
- Argo CLI built with local execution support

## Getting Started

### 1. Start the Local Execution Server

```bash
argo local --port 8080
```

This starts an HTTP server that accepts workflow submissions and executes them using Docker.

### 2. Submit a Workflow

You can submit workflows using `curl` or any HTTP client:

```bash
# Submit a workflow
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @hello-world.yaml

# Or using JSON
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/json" \
  -d @workflow.json
```

### 3. Check Workflow Status

```bash
# List all workflows
curl http://localhost:8080/api/v1/workflows

# Get specific workflow status
curl http://localhost:8080/api/v1/workflows/hello-world-local
```

## Examples

### Hello World

The simplest example that runs a single container:

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @hello-world.yaml
```

### Steps Example

Demonstrates sequential and parallel step execution:

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @steps-example.yaml
```

### Script Example

Shows how to run Python scripts:

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @script-example.yaml
```

## Supported Features

### Currently Supported

- ✅ Container templates
- ✅ Script templates
- ✅ Steps templates (sequential and parallel)
- ✅ DAG templates (basic support)
- ✅ Environment variables
- ✅ Container logs capture
- ✅ Workflow status tracking

### Not Yet Supported

- ❌ Artifacts (input/output)
- ❌ Volumes and PVCs
- ❌ Resource templates
- ❌ Suspend templates
- ❌ DAG dependencies
- ❌ Retry strategies
- ❌ Timeouts
- ❌ Workflow parameters
- ❌ Conditionals

## Architecture

The local execution system consists of three main components:

1. **HTTP Server** (`server.go`): Accepts workflow submissions via REST API
2. **Local Controller** (`local_controller.go`): Manages workflow execution and state
3. **Docker Executor** (`docker_executor.go`): Executes workflow nodes as Docker containers

### Execution Flow

1. User submits workflow manifest to HTTP server
2. Server parses and validates the workflow
3. Local controller analyzes the workflow structure
4. Docker executor creates and runs containers for each node
5. Controller tracks execution status and updates workflow state
6. Results are available via API queries

## Limitations

- Workflows run in a single-threaded manner (no distributed execution)
- No persistence - workflow state is lost when server restarts
- Limited to features that can be mapped to Docker containers
- No authentication or authorization
- Intended for development/testing only, not production use

## Development

To extend local execution support:

1. Add new template type handlers in `local_controller.go`
2. Implement Docker container configurations in `docker_executor.go`
3. Update API endpoints in `server.go` as needed

## Troubleshooting

### Docker Connection Issues

If you see "Cannot connect to Docker daemon" errors:

```bash
# Check Docker is running
docker ps

# On macOS, ensure Docker Desktop is running
# On Linux, check Docker service status
systemctl status docker
```

### Container Execution Failures

Check Docker logs for the specific container:

```bash
# List all containers (including stopped)
docker ps -a

# View logs for a specific container
docker logs <container-id>
```

### Port Already in Use

If port 8080 is already in use:

```bash
# Use a different port
argo local --port 9090
```

## Future Enhancements

Planned improvements for local execution:

- [ ] Artifact support using local filesystem
- [ ] Volume mounts for data sharing
- [ ] Workflow parameters and arguments
- [ ] DAG dependency resolution
- [ ] Retry and timeout support
- [ ] Workflow persistence (SQLite or file-based)
- [ ] Web UI for workflow monitoring
- [ ] Multi-workflow parallel execution
- [ ] Resource limits and quotas

## Contributing

Contributions to improve local execution support are welcome! Please see the main CONTRIBUTING.md for guidelines.
