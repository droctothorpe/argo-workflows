# Getting Started with Local Execution

This guide will help you get started with Argo Workflows Local Execution in just a few minutes.

## What is Local Execution?

Local Execution allows you to run Argo Workflows on your local machine using Docker containers instead of Kubernetes. This is perfect for:

- 🚀 **Rapid Development**: Test workflows quickly without cluster overhead
- 🐛 **Easy Debugging**: Direct access to logs and Docker tools
- 📚 **Learning**: Understand Argo Workflows without Kubernetes complexity
- 💰 **Cost Savings**: No cloud resources needed for development

## Prerequisites

Before you begin, ensure you have:

1. **Docker** installed and running
   ```bash
   docker --version
   # Should show: Docker version 20.x or higher
   ```

2. **Go** installed (for building from source)
   ```bash
   go version
   # Should show: go version go1.24 or higher
   ```

## Installation

### Step 1: Build the Argo CLI

```bash
# Clone the repository (if you haven't already)
git clone https://github.com/argoproj/argo-workflows.git
cd argo-workflows

# Build the CLI with local execution support
make cli

# The binary will be created at dist/argo
./dist/argo version
```

### Step 2: Verify Docker is Running

```bash
docker ps
# Should show running containers (or empty list if none running)
```

## Quick Start

### 1. Start the Local Execution Server

Open a terminal and start the server:

```bash
./dist/argo local
```

You should see:
```
INFO Starting Argo Workflows local execution server  port=8080
INFO Local execution server started successfully
INFO Server is ready to accept workflows  url="http://localhost:8080"
```

Leave this terminal open - the server needs to keep running.

### 2. Submit Your First Workflow

Open a **new terminal** and navigate to the examples directory:

```bash
cd examples/local-execution
```

Submit the hello-world workflow:

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @hello-world.yaml
```

You should see a JSON response showing the workflow was accepted.

### 3. Check the Workflow Status

Wait a few seconds, then check the status:

```bash
curl http://localhost:8080/api/v1/workflows/hello-world-local | jq
```

If you don't have `jq` installed, you can omit it:

```bash
curl http://localhost:8080/api/v1/workflows/hello-world-local
```

You should see the workflow status with `"phase": "Succeeded"`.

### 4. View the Logs

The workflow logs are captured in the node outputs:

```bash
curl -s http://localhost:8080/api/v1/workflows/hello-world-local | \
  jq '.status.nodes[].outputs.parameters[] | select(.name=="logs") | .value'
```

## Using the Test Script

We provide a convenient test script that automates the workflow submission and status checking:

```bash
cd examples/local-execution
chmod +x test-local.sh
./test-local.sh hello-world.yaml
```

The script will:
1. ✅ Check if Docker is running
2. ✅ Check if the server is running
3. ✅ Submit the workflow
4. ✅ Wait for execution
5. ✅ Display the results

## Try More Examples

### Python Script Example

```bash
./test-local.sh script-example.yaml
```

This runs a Python script that generates a random number.

### Steps Example (Sequential & Parallel)

```bash
./test-local.sh steps-example.yaml
```

This demonstrates:
- Sequential step execution
- Parallel step execution within a group

### DAG Example (Parallel Tasks)

```bash
./test-local.sh dag-example.yaml
```

This runs multiple tasks in parallel using a DAG structure.

## Common Operations

### List All Workflows

```bash
curl http://localhost:8080/api/v1/workflows | jq '.items[] | {name: .metadata.name, phase: .status.phase}'
```

### Check Server Health

```bash
curl http://localhost:8080/healthz
```

### Stop the Server

In the terminal where the server is running, press `Ctrl+C`.

### Use a Different Port

```bash
./dist/argo local --port 9090
```

Then use `http://localhost:9090` in your curl commands.

## Creating Your Own Workflow

Create a file called `my-workflow.yaml`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: my-first-workflow
spec:
  entrypoint: main
  templates:
  - name: main
    container:
      image: alpine:latest
      command: [sh, -c]
      args: ["echo 'Hello from my workflow!' && date"]
```

Submit it:

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @my-workflow.yaml
```

Check the status:

```bash
curl http://localhost:8080/api/v1/workflows/my-first-workflow | jq
```

## Troubleshooting

### "Cannot connect to Docker daemon"

**Problem**: Docker is not running.

**Solution**:
- On macOS: Start Docker Desktop
- On Linux: `sudo systemctl start docker`
- Verify: `docker ps`

### "Connection refused" when submitting workflow

**Problem**: The local execution server is not running.

**Solution**:
- Start the server: `./dist/argo local`
- Verify: `curl http://localhost:8080/healthz`

### "Port already in use"

**Problem**: Port 8080 is already being used by another application.

**Solution**:
```bash
# Use a different port
./dist/argo local --port 9090

# Update your curl commands to use the new port
curl http://localhost:9090/api/v1/workflows
```

### Workflow shows "Failed" status

**Problem**: The container failed to execute.

**Solution**:
1. Check the workflow status for error messages:
   ```bash
   curl http://localhost:8080/api/v1/workflows/workflow-name | jq '.status.message'
   ```

2. Check Docker logs:
   ```bash
   docker ps -a | grep argo
   docker logs <container-id>
   ```

3. Verify the container image exists:
   ```bash
   docker pull <image-name>
   ```

## Next Steps

Now that you have local execution running, you can:

1. **Read the Full Documentation**
   - [Local Execution Guide](docs/local-execution.md)
   - [API Reference](docs/local-execution.md#api-reference)

2. **Explore More Examples**
   - All examples are in `examples/local-execution/`
   - Each example demonstrates different features

3. **Learn Argo Workflows**
   - [Official Documentation](https://argo-workflows.readthedocs.io/)
   - [Workflow Concepts](https://argo-workflows.readthedocs.io/en/latest/workflow-concepts/)

4. **Build Complex Workflows**
   - Combine steps and DAGs
   - Use different container images
   - Create multi-stage workflows

5. **Deploy to Kubernetes**
   - Once tested locally, deploy to a real cluster
   - Use the same workflow manifests

## Tips for Success

- 💡 **Start Simple**: Begin with basic container templates
- 🔄 **Iterate Quickly**: Local execution is fast - use it to iterate
- 📝 **Check Logs**: Always review logs when debugging
- 🐳 **Use Docker Tools**: Leverage `docker ps`, `docker logs`, etc.
- 📚 **Read Examples**: Learn from the provided examples
- 🧪 **Test Locally First**: Validate workflows before deploying to K8s

## Getting Help

If you run into issues:

1. **Check the Documentation**
   - [Local Execution Guide](docs/local-execution.md)
   - [Troubleshooting Section](docs/local-execution.md#troubleshooting)

2. **Review Examples**
   - Working examples in `examples/local-execution/`
   - Test script for validation

3. **Ask the Community**
   - GitHub Issues
   - Argo Slack Channel
   - Stack Overflow (tag: argo-workflows)

## What's Next?

You're now ready to develop Argo Workflows locally! Here are some ideas:

- ✨ Create a data processing pipeline
- 🔬 Build a machine learning workflow
- 🚀 Develop a CI/CD pipeline
- 📊 Process and analyze data
- 🧪 Test complex workflow patterns

Happy workflow building! 🎉
