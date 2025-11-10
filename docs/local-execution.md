# Local Execution Mode

Argo Workflows Local Execution mode allows you to run workflows on your local machine using Docker containers instead of Kubernetes. This is ideal for development, testing, and learning Argo Workflows without needing a full Kubernetes cluster.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Architecture](#architecture)
- [API Reference](#api-reference)
- [Supported Features](#supported-features)
- [Limitations](#limitations)
- [Examples](#examples)
- [Troubleshooting](#troubleshooting)

## Overview

Local execution mode provides:

- **Fast iteration**: Test workflows locally without cluster overhead
- **Easy debugging**: Direct access to container logs and Docker tools
- **Simplified development**: No need for kubectl, namespaces, or RBAC
- **Cost-effective**: No cloud resources required for development

## Prerequisites

1. **Docker**: Docker must be installed and running
   ```bash
   docker --version
   docker ps
   ```

2. **Argo CLI**: Build the Argo CLI with local execution support
   ```bash
   make cli
   ```

## Quick Start

### 1. Start the Local Execution Server

```bash
# Start on default port 8080
argo local

# Or specify a custom port
argo local --port 9090
```

The server will start and listen for workflow submissions.

### 2. Submit a Workflow

Create a simple workflow file `hello.yaml`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: hello-local
spec:
  entrypoint: main
  templates:
  - name: main
    container:
      image: alpine:latest
      command: [echo]
      args: ["Hello, Local Execution!"]
```

Submit it to the server:

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @hello.yaml
```

### 3. Check Workflow Status

```bash
# List all workflows
curl http://localhost:8080/api/v1/workflows | jq

# Get specific workflow
curl http://localhost:8080/api/v1/workflows/hello-local | jq
```

## Architecture

The local execution system consists of three main components:

### Components

1. **HTTP Server** (`server.go`)
   - REST API for workflow submission and status queries
   - Handles YAML/JSON workflow parsing
   - Routes requests to the controller

2. **Local Controller** (`local_controller.go`)
   - Manages workflow lifecycle
   - Orchestrates template execution
   - Tracks workflow and node status
   - Handles steps and DAG execution

3. **Docker Executor** (`docker_executor.go`)
   - Creates and manages Docker containers
   - Maps workflow templates to container configurations
   - Captures logs and exit codes
   - Cleans up containers after execution

### Execution Flow

```
User → HTTP API → Local Controller → Docker Executor → Docker Containers
                        ↓
                   Status Tracking
                        ↓
                   HTTP API Response
```

## API Reference

### Submit Workflow

**POST** `/api/v1/workflows`

Submit a new workflow for execution.

**Headers:**
- `Content-Type: application/yaml` or `application/json`

**Request Body:**
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: my-workflow
spec:
  entrypoint: main
  templates:
  - name: main
    container:
      image: alpine:latest
      command: [echo, "hello"]
```

**Response:** 201 Created
```json
{
  "metadata": {
    "name": "my-workflow"
  },
  "status": {
    "phase": "Pending",
    "startedAt": "2024-01-01T00:00:00Z"
  }
}
```

### List Workflows

**GET** `/api/v1/workflows`

List all workflows.

**Response:** 200 OK
```json
{
  "items": [
    {
      "metadata": {
        "name": "my-workflow"
      },
      "status": {
        "phase": "Succeeded"
      }
    }
  ]
}
```

### Get Workflow

**GET** `/api/v1/workflows/{name}`

Get a specific workflow by name.

**Response:** 200 OK
```json
{
  "metadata": {
    "name": "my-workflow"
  },
  "status": {
    "phase": "Succeeded",
    "startedAt": "2024-01-01T00:00:00Z",
    "finishedAt": "2024-01-01T00:00:30Z",
    "nodes": {
      "my-workflow": {
        "phase": "Succeeded",
        "outputs": {
          "parameters": [
            {
              "name": "logs",
              "value": "hello\n"
            }
          ]
        }
      }
    }
  }
}
```

### Health Check

**GET** `/healthz`

Check server health.

**Response:** 200 OK
```json
{
  "status": "healthy"
}
```

## Supported Features

### Template Types

- ✅ **Container**: Run a container with specified image and commands
- ✅ **Script**: Execute inline scripts in a container
- ✅ **Steps**: Sequential and parallel step execution
- ✅ **DAG**: Directed Acyclic Graph workflows (basic support)

### Workflow Features

- ✅ Environment variables
- ✅ Container logs capture
- ✅ Workflow status tracking
- ✅ Node status tracking
- ✅ Parallel execution (steps and DAG)
- ✅ Sequential execution (steps)

### Container Features

- ✅ Custom images
- ✅ Commands and arguments
- ✅ Environment variables
- ✅ Working directory

## Limitations

The following features are **not yet supported** in local execution mode:

- ❌ Artifacts (input/output)
- ❌ Volumes and PersistentVolumeClaims
- ❌ Resource templates
- ❌ Suspend templates
- ❌ DAG task dependencies
- ❌ Retry strategies
- ❌ Timeout configurations
- ❌ Workflow parameters and arguments
- ❌ Conditionals (when expressions)
- ❌ Workflow templates and cluster workflow templates
- ❌ Cron workflows
- ❌ Workflow persistence (state lost on restart)
- ❌ Authentication and authorization
- ❌ Metrics and monitoring

## Examples

### Container Template

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: container-example
spec:
  entrypoint: main
  templates:
  - name: main
    container:
      image: alpine:latest
      command: [sh, -c]
      args: ["echo Hello && sleep 2 && echo World"]
```

### Script Template

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: script-example
spec:
  entrypoint: main
  templates:
  - name: main
    script:
      image: python:alpine3.6
      command: [python]
      source: |
        print("Hello from Python!")
        for i in range(5):
            print(f"Count: {i}")
```

### Steps Template

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: steps-example
spec:
  entrypoint: main
  templates:
  - name: main
    steps:
    - - name: step1
        template: echo
    - - name: step2a
        template: echo
      - name: step2b
        template: echo
    - - name: step3
        template: echo
  
  - name: echo
    container:
      image: alpine:latest
      command: [echo]
      args: ["Step executed"]
```

### DAG Template

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: dag-example
spec:
  entrypoint: main
  templates:
  - name: main
    dag:
      tasks:
      - name: task-a
        template: echo
      - name: task-b
        template: echo
      - name: task-c
        template: echo
  
  - name: echo
    container:
      image: alpine:latest
      command: [echo]
      args: ["Task executed"]
```

## Troubleshooting

### Docker Connection Issues

**Problem:** `Cannot connect to Docker daemon`

**Solution:**
```bash
# Check Docker is running
docker ps

# On macOS, ensure Docker Desktop is running
# On Linux, start Docker service
sudo systemctl start docker

# Check Docker socket permissions
ls -la /var/run/docker.sock
```

### Port Already in Use

**Problem:** `bind: address already in use`

**Solution:**
```bash
# Use a different port
argo local --port 9090

# Or find and stop the process using port 8080
lsof -i :8080
kill <PID>
```

### Workflow Submission Fails

**Problem:** `Failed to parse workflow`

**Solution:**
- Ensure YAML is valid: `yamllint workflow.yaml`
- Check required fields: `name`, `entrypoint`, `templates`
- Validate against Argo Workflows schema

### Container Execution Fails

**Problem:** Workflow shows `Failed` status

**Solution:**
```bash
# Check Docker logs
docker ps -a | grep argo
docker logs <container-id>

# Verify image exists and is accessible
docker pull <image-name>

# Check container exit code in workflow status
curl http://localhost:8080/api/v1/workflows/<workflow-name> | jq '.status.nodes'
```

### No Logs in Output

**Problem:** Node status doesn't show logs

**Solution:**
- Logs are captured after container completes
- Check that container produces stdout/stderr
- Verify Docker logging driver is configured correctly

## Development

### Building from Source

```bash
# Build the CLI
make cli

# Run tests
go test ./workflow/executor/local/...

# Build with race detection
go build -race -o dist/argo ./cmd/argo
```

### Extending Local Execution

To add support for new features:

1. **Add template type support**: Modify `local_controller.go`
2. **Enhance Docker execution**: Update `docker_executor.go`
3. **Add API endpoints**: Extend `server.go`
4. **Add tests**: Create test files in the `local` package

### Testing

```bash
# Start the server
argo local --port 8080

# Run the test script
cd examples/local-execution
./test-local.sh hello-world.yaml

# Test with different workflows
./test-local.sh steps-example.yaml
./test-local.sh script-example.yaml
```

## Future Roadmap

Planned enhancements:

1. **Artifact Support**: Local filesystem-based artifact storage
2. **Volume Mounts**: Support for volume mounts and data sharing
3. **Parameters**: Workflow and template parameters
4. **Dependencies**: DAG task dependency resolution
5. **Retries**: Automatic retry on failure
6. **Timeouts**: Template and workflow timeouts
7. **Persistence**: SQLite-based workflow state persistence
8. **Web UI**: Browser-based workflow monitoring
9. **Resource Limits**: CPU and memory constraints
10. **Authentication**: Basic auth for API access

## Contributing

Contributions are welcome! Please see the main [CONTRIBUTING.md](../CONTRIBUTING.md) for guidelines.

For local execution specific contributions:
- Add tests for new features
- Update documentation
- Follow existing code patterns
- Ensure Docker compatibility

## See Also

- [Argo Workflows Documentation](https://argo-workflows.readthedocs.io/)
- [Docker SDK for Go](https://docs.docker.com/engine/api/sdk/)
- [Examples](../examples/local-execution/)
