# Quick Start Guide - Local Execution

Get started with Argo Workflows Local Execution in 5 minutes!

## Prerequisites

- Docker installed and running
- Argo CLI built from source

## Step 1: Build the CLI

```bash
cd /path/to/argo-workflows
make cli
```

This creates the `argo` binary in `dist/argo`.

## Step 2: Start the Local Server

```bash
./dist/argo local
```

You should see:
```
INFO[0000] Starting Argo Workflows local execution server  port=8080
INFO[0000] Local execution server started successfully
INFO[0000] Server is ready to accept workflows          url="http://localhost:8080"
```

## Step 3: Submit Your First Workflow

Open a new terminal and submit the hello-world example:

```bash
cd examples/local-execution

curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @hello-world.yaml
```

You should see a JSON response with the workflow details.

## Step 4: Check Workflow Status

```bash
curl http://localhost:8080/api/v1/workflows/hello-world-local | jq
```

Output:
```json
{
  "metadata": {
    "name": "hello-world-local"
  },
  "status": {
    "phase": "Succeeded",
    "startedAt": "2024-01-01T00:00:00Z",
    "finishedAt": "2024-01-01T00:00:05Z",
    "nodes": {
      "hello-world-local": {
        "id": "hello-world-local",
        "name": "whalesay",
        "phase": "Succeeded",
        "outputs": {
          "parameters": [
            {
              "name": "logs",
              "value": "Hello from Argo Workflows Local Execution!"
            }
          ]
        }
      }
    }
  }
}
```

## Step 5: Try More Examples

### Script Example (Python)

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @script-example.yaml
```

### Steps Example (Sequential & Parallel)

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @steps-example.yaml
```

### DAG Example (Parallel Tasks)

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @dag-example.yaml
```

## Step 6: List All Workflows

```bash
curl http://localhost:8080/api/v1/workflows | jq
```

## Using the Test Script

We provide a convenient test script:

```bash
chmod +x test-local.sh
./test-local.sh hello-world.yaml
```

The script will:
1. Check if Docker is running
2. Check if the server is running
3. Submit the workflow
4. Wait for execution
5. Display the results

## Common Commands

### Check Server Health

```bash
curl http://localhost:8080/healthz
```

### Submit Workflow (JSON)

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/json" \
  -d '{
    "metadata": {"name": "my-workflow"},
    "spec": {
      "entrypoint": "main",
      "templates": [{
        "name": "main",
        "container": {
          "image": "alpine:latest",
          "command": ["echo"],
          "args": ["Hello!"]
        }
      }]
    }
  }'
```

### Pretty Print Status

```bash
curl -s http://localhost:8080/api/v1/workflows/my-workflow | jq '.status'
```

### Watch Docker Containers

In another terminal, watch containers being created:

```bash
watch -n 1 'docker ps -a | grep argo'
```

## Troubleshooting

### Server Won't Start

**Error:** `bind: address already in use`

**Solution:** Use a different port:
```bash
./dist/argo local --port 9090
```

### Docker Not Running

**Error:** `Cannot connect to Docker daemon`

**Solution:**
```bash
# Start Docker Desktop (macOS)
# Or start Docker service (Linux)
sudo systemctl start docker
```

### Workflow Submission Fails

**Error:** `Failed to parse workflow`

**Solution:** Validate your YAML:
```bash
yamllint workflow.yaml
```

Ensure required fields are present:
- `metadata.name`
- `spec.entrypoint`
- `spec.templates`

## Next Steps

1. **Read the Full Documentation**: See [docs/local-execution.md](../../docs/local-execution.md)
2. **Explore Examples**: Check out all examples in this directory
3. **Create Your Own Workflows**: Start building custom workflows
4. **Learn Argo Workflows**: Visit [argo-workflows.readthedocs.io](https://argo-workflows.readthedocs.io/)

## Tips

- **Use jq**: Install `jq` for better JSON formatting
- **Watch Logs**: Use `docker logs <container-id>` to debug
- **Iterate Quickly**: Local execution is perfect for rapid development
- **Test Before Deploy**: Validate workflows locally before deploying to Kubernetes

## Getting Help

- **Documentation**: [docs/local-execution.md](../../docs/local-execution.md)
- **Examples**: All files in `examples/local-execution/`
- **Issues**: Report bugs on GitHub
- **Community**: Join the Argo Slack channel

Happy workflow building! 🚀
