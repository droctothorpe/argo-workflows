package local

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/util/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DockerExecutor executes workflow nodes as Docker containers
type DockerExecutor struct {
	client *client.Client
	logger logging.Logger
}

// NewDockerExecutor creates a new Docker-based executor
func NewDockerExecutor(ctx context.Context) (*DockerExecutor, error) {
	// Create Docker client with options that work with both Docker Desktop and Colima
	// client.FromEnv will read DOCKER_HOST, DOCKER_CERT_PATH, etc.
	// client.WithHostFromEnv() ensures we use the Docker context if available
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
		client.WithHostFromEnv(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	// Verify connection to Docker daemon
	_, err = cli.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker daemon: %w (hint: ensure Docker is running and DOCKER_HOST is set if using Colima)", err)
	}

	return &DockerExecutor{
		client: cli,
		logger: logging.RequireLoggerFromContext(ctx),
	}, nil
}

// ExecuteContainer executes a workflow node as a Docker container
func (de *DockerExecutor) ExecuteContainer(ctx context.Context, nodeName string, tmpl *wfv1.Template, wf *wfv1.Workflow) (*wfv1.NodeStatus, error) {
	logger := de.logger.WithField("nodeName", nodeName)
	logger.Info(ctx, "Executing container node")

	if tmpl.Container == nil {
		return nil, fmt.Errorf("template %s does not have a container spec", tmpl.Name)
	}

	// Build container configuration
	containerConfig := &container.Config{
		Image:        tmpl.Container.Image,
		Cmd:          tmpl.Container.Command,
		Env:          de.buildEnvVars(tmpl, wf, nodeName),
		WorkingDir:   tmpl.Container.WorkingDir,
		AttachStdout: true,
		AttachStderr: true,
	}

	// Add args if present
	if len(tmpl.Container.Args) > 0 {
		containerConfig.Cmd = append(containerConfig.Cmd, tmpl.Container.Args...)
	}

	// Build host configuration
	hostConfig := &container.HostConfig{
		AutoRemove: false, // We want to inspect the container after it exits
	}

	// Add volume mounts
	if len(tmpl.Container.VolumeMounts) > 0 {
		mounts := []mount.Mount{}
		for _, vm := range tmpl.Container.VolumeMounts {
			// For local execution, we'll use bind mounts
			mounts = append(mounts, mount.Mount{
				Type:   mount.TypeBind,
				Source: fmt.Sprintf("/tmp/argo-local/%s/%s", wf.Name, vm.Name),
				Target: vm.MountPath,
			})
		}
		hostConfig.Mounts = mounts
	}

	// Create container name
	containerName := fmt.Sprintf("argo-%s-%s", wf.Name, nodeName)
	containerName = strings.ReplaceAll(containerName, ".", "-")

	logger.WithField("containerName", containerName).Debug(ctx, "Creating Docker container")

	// Create the container
	resp, err := de.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	logger.WithField("containerID", resp.ID).Info(ctx, "Container created")

	// Start the container
	if err := de.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	logger.Info(ctx, "Container started")

	// Initialize node status
	nodeStatus := &wfv1.NodeStatus{
		ID:          wf.NodeID(nodeName),
		Name:        nodeName,
		DisplayName: nodeName,
		Type:        wfv1.NodeTypePod,
		Phase:       wfv1.NodeRunning,
		StartedAt:   metav1.Time{Time: time.Now()},
		HostNodeName: containerName,
	}

	// Wait for container to complete
	statusCh, errCh := de.client.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			nodeStatus.Phase = wfv1.NodeError
			nodeStatus.Message = err.Error()
			return nodeStatus, err
		}
	case status := <-statusCh:
		logger.WithField("exitCode", status.StatusCode).Info(ctx, "Container completed")
		
		nodeStatus.FinishedAt = metav1.Time{Time: time.Now()}
		
		if status.StatusCode == 0 {
			nodeStatus.Phase = wfv1.NodeSucceeded
		} else {
			nodeStatus.Phase = wfv1.NodeFailed
			nodeStatus.Message = fmt.Sprintf("container exited with code %d", status.StatusCode)
		}

		// Capture logs
		logs, err := de.getContainerLogs(ctx, resp.ID)
		if err != nil {
			logger.WithError(err).Warn(ctx, "Failed to capture container logs")
		} else {
			nodeStatus.Outputs = &wfv1.Outputs{
				Parameters: []wfv1.Parameter{
					{
						Name:  "logs",
						Value: wfv1.AnyStringPtr(logs),
					},
				},
			}
		}
	}

	// Cleanup container
	if err := de.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true}); err != nil {
		logger.WithError(err).Warn(ctx, "Failed to remove container")
	}

	return nodeStatus, nil
}

// ExecuteScript executes a script template as a Docker container
func (de *DockerExecutor) ExecuteScript(ctx context.Context, nodeName string, tmpl *wfv1.Template, wf *wfv1.Workflow) (*wfv1.NodeStatus, error) {
	logger := de.logger.WithField("nodeName", nodeName)
	logger.Info(ctx, "Executing script node")

	if tmpl.Script == nil {
		return nil, fmt.Errorf("template %s does not have a script spec", tmpl.Name)
	}

	// Build container configuration with script
	containerConfig := &container.Config{
		Image:        tmpl.Script.Image,
		Cmd:          []string{tmpl.Script.Command[0]},
		Env:          de.buildEnvVars(tmpl, wf, nodeName),
		WorkingDir:   tmpl.Script.WorkingDir,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
	}

	// Add script as stdin or command
	if len(tmpl.Script.Command) > 1 {
		containerConfig.Cmd = append(containerConfig.Cmd, tmpl.Script.Command[1:]...)
	}
	containerConfig.Cmd = append(containerConfig.Cmd, "-c", tmpl.Script.Source)

	hostConfig := &container.HostConfig{
		AutoRemove: false,
	}

	containerName := fmt.Sprintf("argo-%s-%s", wf.Name, nodeName)
	containerName = strings.ReplaceAll(containerName, ".", "-")

	logger.WithField("containerName", containerName).Debug(ctx, "Creating Docker container for script")

	resp, err := de.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	if err := de.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	nodeStatus := &wfv1.NodeStatus{
		ID:          wf.NodeID(nodeName),
		Name:        nodeName,
		DisplayName: nodeName,
		Type:        wfv1.NodeTypePod,
		Phase:       wfv1.NodeRunning,
		StartedAt:   metav1.Time{Time: time.Now()},
		HostNodeName: containerName,
	}

	statusCh, errCh := de.client.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			nodeStatus.Phase = wfv1.NodeError
			nodeStatus.Message = err.Error()
			return nodeStatus, err
		}
	case status := <-statusCh:
		nodeStatus.FinishedAt = metav1.Time{Time: time.Now()}
		
		if status.StatusCode == 0 {
			nodeStatus.Phase = wfv1.NodeSucceeded
		} else {
			nodeStatus.Phase = wfv1.NodeFailed
			nodeStatus.Message = fmt.Sprintf("script exited with code %d", status.StatusCode)
		}

		logs, err := de.getContainerLogs(ctx, resp.ID)
		if err != nil {
			logger.WithError(err).Warn(ctx, "Failed to capture container logs")
		} else {
			nodeStatus.Outputs = &wfv1.Outputs{
				Parameters: []wfv1.Parameter{
					{
						Name:  "logs",
						Value: wfv1.AnyStringPtr(logs),
					},
				},
			}
		}
	}

	if err := de.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true}); err != nil {
		logger.WithError(err).Warn(ctx, "Failed to remove container")
	}

	return nodeStatus, nil
}

// buildEnvVars builds environment variables for the container
func (de *DockerExecutor) buildEnvVars(tmpl *wfv1.Template, wf *wfv1.Workflow, nodeName string) []string {
	envVars := []string{
		fmt.Sprintf("ARGO_WORKFLOW_NAME=%s", wf.Name),
		fmt.Sprintf("ARGO_NODE_NAME=%s", nodeName),
		fmt.Sprintf("ARGO_TEMPLATE_NAME=%s", tmpl.Name),
	}

	// Add template-specific env vars
	if tmpl.Container != nil {
		for _, env := range tmpl.Container.Env {
			envVars = append(envVars, fmt.Sprintf("%s=%s", env.Name, env.Value))
		}
	}

	if tmpl.Script != nil {
		for _, env := range tmpl.Script.Env {
			envVars = append(envVars, fmt.Sprintf("%s=%s", env.Name, env.Value))
		}
	}

	return envVars
}

// getContainerLogs retrieves logs from a container
func (de *DockerExecutor) getContainerLogs(ctx context.Context, containerID string) (string, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	}

	reader, err := de.client.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	logs, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return string(logs), nil
}

// Close closes the Docker client
func (de *DockerExecutor) Close() error {
	if de.client != nil {
		return de.client.Close()
	}
	return nil
}

// InspectContainer returns information about a container
func (de *DockerExecutor) InspectContainer(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	return de.client.ContainerInspect(ctx, containerID)
}

// ListContainers lists all containers with the argo prefix
func (de *DockerExecutor) ListContainers(ctx context.Context) ([]types.Container, error) {
	return de.client.ContainerList(ctx, container.ListOptions{
		All: true,
	})
}
