# Local Execution Package

This package implements local execution support for Argo Workflows, allowing workflows to run on a local machine using Docker containers instead of Kubernetes.

## Package Structure

```
local/
├── docker_executor.go    # Docker container execution logic
├── local_controller.go   # Workflow orchestration and state management
├── server.go            # HTTP API server
└── README.md           # This file
```

## Components

### DockerExecutor (`docker_executor.go`)

Handles the low-level Docker container operations:

- **Container Creation**: Builds Docker container configurations from workflow templates
- **Execution**: Starts and monitors container execution
- **Log Capture**: Retrieves stdout/stderr from containers
- **Cleanup**: Removes containers after execution

**Key Methods:**
- `ExecuteContainer()`: Executes a container template
- `ExecuteScript()`: Executes a script template
- `buildEnvVars()`: Constructs environment variables for containers
- `getContainerLogs()`: Retrieves container logs

### LocalController (`local_controller.go`)

Manages workflow lifecycle and orchestration:

- **Workflow Submission**: Accepts and validates workflow manifests
- **Template Resolution**: Finds and resolves template references
- **Execution Orchestration**: Coordinates execution of different template types
- **State Management**: Tracks workflow and node status

**Key Methods:**
- `SubmitWorkflow()`: Submits a workflow for execution
- `executeWorkflow()`: Main workflow execution loop
- `executeTemplate()`: Routes template execution based on type
- `executeContainerTemplate()`: Handles container templates
- `executeScriptTemplate()`: Handles script templates
- `executeStepsTemplate()`: Handles steps templates (sequential/parallel)
- `executeDAGTemplate()`: Handles DAG templates

### Server (`server.go`)

Provides HTTP API for workflow operations:

- **REST API**: HTTP endpoints for workflow submission and queries
- **Request Handling**: Parses YAML/JSON workflow manifests
- **Response Formatting**: Returns workflow status as JSON

**API Endpoints:**
- `POST /api/v1/workflows`: Submit a workflow
- `GET /api/v1/workflows`: List all workflows
- `GET /api/v1/workflows/{name}`: Get workflow status
- `GET /healthz`: Health check

## Usage

### Starting the Server

```go
import (
    "context"
    "github.com/argoproj/argo-workflows/v3/workflow/executor/local"
)

func main() {
    ctx := context.Background()
    server, err := local.NewServer(ctx, 8080)
    if err != nil {
        panic(err)
    }
    defer server.Close()
    
    if err := server.Start(ctx); err != nil {
        panic(err)
    }
}
```

### Submitting a Workflow Programmatically

```go
import (
    wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
    "github.com/argoproj/argo-workflows/v3/workflow/executor/local"
)

func submitWorkflow(ctx context.Context) error {
    controller, err := local.NewLocalController(ctx)
    if err != nil {
        return err
    }
    defer controller.Close()
    
    wf := &wfv1.Workflow{
        ObjectMeta: metav1.ObjectMeta{
            Name: "my-workflow",
        },
        Spec: wfv1.WorkflowSpec{
            Entrypoint: "main",
            Templates: []wfv1.Template{
                {
                    Name: "main",
                    Container: &corev1.Container{
                        Image:   "alpine:latest",
                        Command: []string{"echo"},
                        Args:    []string{"Hello!"},
                    },
                },
            },
        },
    }
    
    return controller.SubmitWorkflow(ctx, wf)
}
```

## Implementation Details

### Container Naming

Containers are named using the pattern: `argo-{workflow-name}-{node-name}`

Special characters (dots, etc.) are replaced with hyphens to ensure Docker compatibility.

### Environment Variables

The following environment variables are automatically injected into containers:

- `ARGO_WORKFLOW_NAME`: Name of the workflow
- `ARGO_NODE_NAME`: Name of the current node
- `ARGO_TEMPLATE_NAME`: Name of the template being executed

Additional environment variables from the template are also included.

### Log Capture

Container logs (stdout and stderr) are captured after execution completes and stored in the node's output parameters under the key `logs`.

### Parallel Execution

- **Steps**: Steps in the same group execute in parallel using goroutines
- **DAG**: All tasks execute in parallel (dependencies not yet implemented)

### Error Handling

- Container exit code 0 = Node succeeds
- Non-zero exit code = Node fails
- Docker errors = Node enters error state

## Limitations

### Current Limitations

1. **No Artifact Support**: Input/output artifacts are not implemented
2. **No Volume Mounts**: PVCs and volumes are not supported
3. **No Persistence**: Workflow state is lost on server restart
4. **No Dependencies**: DAG task dependencies are not enforced
5. **No Retries**: Retry strategies are not implemented
6. **No Timeouts**: Template timeouts are not enforced
7. **No Parameters**: Workflow parameters are not supported
8. **No Conditionals**: When expressions are not evaluated

### Design Decisions

**Why Docker?**
- Widely available on developer machines
- Familiar to most developers
- Good isolation and resource management
- Compatible with container images used in Kubernetes

**Why HTTP API?**
- Simple to use with curl or any HTTP client
- Easy to integrate with other tools
- No need for Kubernetes client libraries
- Stateless server design

**Why In-Memory State?**
- Simplifies implementation
- Fast access to workflow state
- Sufficient for development/testing use cases
- Can be extended to persistent storage later

## Testing

### Unit Tests

```bash
go test ./workflow/executor/local/...
```

### Integration Tests

```bash
# Start the server
argo local --port 8080

# Run test script
cd examples/local-execution
./test-local.sh hello-world.yaml
```

### Manual Testing

```bash
# Start server
argo local

# Submit workflow
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @workflow.yaml

# Check status
curl http://localhost:8080/api/v1/workflows/workflow-name | jq
```

## Future Enhancements

### Planned Features

1. **Artifact Support**
   - Local filesystem-based artifact storage
   - Artifact passing between steps
   - Archive location configuration

2. **Volume Support**
   - Bind mounts for local directories
   - Temporary volumes for workflow execution
   - Volume sharing between containers

3. **Parameter Support**
   - Workflow-level parameters
   - Template-level parameters
   - Parameter passing between templates

4. **Dependency Resolution**
   - DAG task dependencies
   - Conditional execution (when expressions)
   - Dependency-based scheduling

5. **Retry and Timeout**
   - Automatic retry on failure
   - Configurable retry strategies
   - Template and workflow timeouts

6. **Persistence**
   - SQLite-based workflow storage
   - Resume workflows after restart
   - Workflow history and audit log

7. **Advanced Features**
   - Resource limits (CPU, memory)
   - Workflow templates and cluster workflow templates
   - Workflow hooks (lifecycle events)
   - Metrics and monitoring

### Contributing

To contribute to local execution:

1. **Add Tests**: Write unit tests for new features
2. **Update Documentation**: Keep docs in sync with code
3. **Follow Patterns**: Use existing code patterns
4. **Docker Compatibility**: Ensure features work with Docker

## Dependencies

- `github.com/docker/docker`: Docker SDK for Go
- `github.com/argoproj/argo-workflows/v3/pkg/apis/workflow`: Workflow types
- `github.com/argoproj/argo-workflows/v3/util/logging`: Logging utilities
- `sigs.k8s.io/yaml`: YAML parsing

## See Also

- [Local Execution Documentation](../../../docs/local-execution.md)
- [Examples](../../../examples/local-execution/)
- [Docker SDK Documentation](https://docs.docker.com/engine/api/sdk/)
