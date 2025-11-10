package local

import (
	"context"
	"fmt"
	"sync"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/util/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LocalController manages workflow execution in local mode using Docker
type LocalController struct {
	executor  *DockerExecutor
	workflows map[string]*WorkflowExecution
	mu        sync.RWMutex
	logger    logging.Logger
}

// WorkflowExecution tracks the execution state of a workflow
type WorkflowExecution struct {
	Workflow *wfv1.Workflow
	Status   *wfv1.WorkflowStatus
	mu       sync.RWMutex
}

// NewLocalController creates a new local workflow controller
func NewLocalController(ctx context.Context) (*LocalController, error) {
	executor, err := NewDockerExecutor(ctx)
	if err != nil {
		return nil, err
	}

	return &LocalController{
		executor:  executor,
		workflows: make(map[string]*WorkflowExecution),
		logger:    logging.RequireLoggerFromContext(ctx),
	}, nil
}

// SubmitWorkflow submits a workflow for execution
func (lc *LocalController) SubmitWorkflow(ctx context.Context, wf *wfv1.Workflow) error {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	logger := lc.logger.WithField("workflow", wf.Name)
	logger.Info(ctx, "Submitting workflow for local execution")

	// Initialize workflow status
	if wf.Status.Phase == "" {
		wf.Status.Phase = wfv1.WorkflowPending
		wf.Status.StartedAt = metav1.Time{Time: time.Now()}
	}

	wfExec := &WorkflowExecution{
		Workflow: wf,
		Status:   &wf.Status,
	}

	lc.workflows[wf.Name] = wfExec

	// Start workflow execution in background with a new context
	// Use context.Background() so execution continues after HTTP request completes
	go lc.executeWorkflow(context.Background(), wfExec)

	return nil
}

// executeWorkflow executes a workflow
func (lc *LocalController) executeWorkflow(ctx context.Context, wfExec *WorkflowExecution) {
	wf := wfExec.Workflow
	logger := lc.logger.WithField("workflow", wf.Name)
	
	logger.Info(ctx, "Starting workflow execution")
	
	wfExec.mu.Lock()
	wfExec.Status.Phase = wfv1.WorkflowRunning
	wfExec.mu.Unlock()

	// Initialize nodes map if not exists
	if wfExec.Status.Nodes == nil {
		wfExec.Status.Nodes = make(map[string]wfv1.NodeStatus)
	}

	// Find the entrypoint template
	entrypoint := wf.Spec.Entrypoint
	if entrypoint == "" {
		logger.Error(ctx, "No entrypoint specified in workflow")
		lc.markWorkflowFailed(wfExec, "No entrypoint specified")
		return
	}

	// Find the template
	var entrypointTmpl *wfv1.Template
	for i := range wf.Spec.Templates {
		if wf.Spec.Templates[i].Name == entrypoint {
			entrypointTmpl = &wf.Spec.Templates[i]
			break
		}
	}

	if entrypointTmpl == nil {
		logger.WithField("entrypoint", entrypoint).Error(ctx, "Entrypoint template not found")
		lc.markWorkflowFailed(wfExec, fmt.Sprintf("Entrypoint template '%s' not found", entrypoint))
		return
	}

	// Execute based on template type
	if err := lc.executeTemplate(ctx, wfExec, entrypoint, entrypointTmpl); err != nil {
		logger.WithError(err).Error(ctx, "Failed to execute workflow")
		lc.markWorkflowFailed(wfExec, err.Error())
		return
	}

	// Mark workflow as succeeded
	wfExec.mu.Lock()
	wfExec.Status.Phase = wfv1.WorkflowSucceeded
	wfExec.Status.FinishedAt = metav1.Time{Time: time.Now()}
	wfExec.mu.Unlock()

	logger.Info(ctx, "Workflow completed successfully")
}

// executeTemplate executes a single template
func (lc *LocalController) executeTemplate(ctx context.Context, wfExec *WorkflowExecution, nodeName string, tmpl *wfv1.Template) error {
	logger := lc.logger.WithFields(logging.Fields{
		"workflow": wfExec.Workflow.Name,
		"node":     nodeName,
		"template": tmpl.Name,
	})

	logger.Info(ctx, "Executing template")

	// Handle different template types
	switch {
	case tmpl.Container != nil:
		return lc.executeContainerTemplate(ctx, wfExec, nodeName, tmpl)
	case tmpl.Script != nil:
		return lc.executeScriptTemplate(ctx, wfExec, nodeName, tmpl)
	case tmpl.Steps != nil:
		return lc.executeStepsTemplate(ctx, wfExec, nodeName, tmpl)
	case tmpl.DAG != nil:
		return lc.executeDAGTemplate(ctx, wfExec, nodeName, tmpl)
	default:
		return fmt.Errorf("unsupported template type for template %s", tmpl.Name)
	}
}

// executeContainerTemplate executes a container template
func (lc *LocalController) executeContainerTemplate(ctx context.Context, wfExec *WorkflowExecution, nodeName string, tmpl *wfv1.Template) error {
	nodeStatus, err := lc.executor.ExecuteContainer(ctx, nodeName, tmpl, wfExec.Workflow)
	if err != nil {
		return err
	}

	wfExec.mu.Lock()
	wfExec.Status.Nodes[nodeStatus.ID] = *nodeStatus
	wfExec.mu.Unlock()

	if nodeStatus.Phase == wfv1.NodeFailed || nodeStatus.Phase == wfv1.NodeError {
		return fmt.Errorf("container execution failed: %s", nodeStatus.Message)
	}

	return nil
}

// executeScriptTemplate executes a script template
func (lc *LocalController) executeScriptTemplate(ctx context.Context, wfExec *WorkflowExecution, nodeName string, tmpl *wfv1.Template) error {
	nodeStatus, err := lc.executor.ExecuteScript(ctx, nodeName, tmpl, wfExec.Workflow)
	if err != nil {
		return err
	}

	wfExec.mu.Lock()
	wfExec.Status.Nodes[nodeStatus.ID] = *nodeStatus
	wfExec.mu.Unlock()

	if nodeStatus.Phase == wfv1.NodeFailed || nodeStatus.Phase == wfv1.NodeError {
		return fmt.Errorf("script execution failed: %s", nodeStatus.Message)
	}

	return nil
}

// executeStepsTemplate executes a steps template
func (lc *LocalController) executeStepsTemplate(ctx context.Context, wfExec *WorkflowExecution, nodeName string, tmpl *wfv1.Template) error {
	logger := lc.logger.WithField("template", tmpl.Name)
	logger.Info(ctx, "Executing steps template")

	// Execute steps sequentially
	for i, stepGroup := range tmpl.Steps {
		logger.WithField("stepGroup", i).Debug(ctx, "Executing step group")
		
		// Steps in the same group can run in parallel
		var wg sync.WaitGroup
		errChan := make(chan error, len(stepGroup.Steps))

		for j, step := range stepGroup.Steps {
			wg.Add(1)
			go func(stepIndex int, s wfv1.WorkflowStep) {
				defer wg.Done()

				stepNodeName := fmt.Sprintf("%s[%d].%s", nodeName, i, s.Name)
				
				// Find the referenced template
				var stepTmpl *wfv1.Template
				for k := range wfExec.Workflow.Spec.Templates {
					if wfExec.Workflow.Spec.Templates[k].Name == s.Template {
						stepTmpl = &wfExec.Workflow.Spec.Templates[k]
						break
					}
				}

				if stepTmpl == nil {
					errChan <- fmt.Errorf("template '%s' not found for step '%s'", s.Template, s.Name)
					return
				}

				if err := lc.executeTemplate(ctx, wfExec, stepNodeName, stepTmpl); err != nil {
					errChan <- err
				}
			}(j, step)
		}

		wg.Wait()
		close(errChan)

		// Check for errors
		for err := range errChan {
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// executeDAGTemplate executes a DAG template
func (lc *LocalController) executeDAGTemplate(ctx context.Context, wfExec *WorkflowExecution, nodeName string, tmpl *wfv1.Template) error {
	logger := lc.logger.WithField("template", tmpl.Name)
	logger.Info(ctx, "Executing DAG template")

	// Simple DAG execution - run all tasks in parallel (ignoring dependencies for now)
	var wg sync.WaitGroup
	errChan := make(chan error, len(tmpl.DAG.Tasks))

	for _, task := range tmpl.DAG.Tasks {
		wg.Add(1)
		go func(t wfv1.DAGTask) {
			defer wg.Done()

			taskNodeName := fmt.Sprintf("%s.%s", nodeName, t.Name)
			
			// Find the referenced template
			var taskTmpl *wfv1.Template
			for k := range wfExec.Workflow.Spec.Templates {
				if wfExec.Workflow.Spec.Templates[k].Name == t.Template {
					taskTmpl = &wfExec.Workflow.Spec.Templates[k]
					break
				}
			}

			if taskTmpl == nil {
				errChan <- fmt.Errorf("template '%s' not found for task '%s'", t.Template, t.Name)
				return
			}

			if err := lc.executeTemplate(ctx, wfExec, taskNodeName, taskTmpl); err != nil {
				errChan <- err
			}
		}(task)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}

// markWorkflowFailed marks a workflow as failed
func (lc *LocalController) markWorkflowFailed(wfExec *WorkflowExecution, message string) {
	wfExec.mu.Lock()
	defer wfExec.mu.Unlock()

	wfExec.Status.Phase = wfv1.WorkflowFailed
	wfExec.Status.Message = message
	wfExec.Status.FinishedAt = metav1.Time{Time: time.Now()}
}

// GetWorkflow retrieves a workflow by name
func (lc *LocalController) GetWorkflow(name string) (*wfv1.Workflow, error) {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	wfExec, exists := lc.workflows[name]
	if !exists {
		return nil, fmt.Errorf("workflow '%s' not found", name)
	}

	wfExec.mu.RLock()
	defer wfExec.mu.RUnlock()

	// Create a copy with updated status
	wf := wfExec.Workflow.DeepCopy()
	wf.Status = *wfExec.Status
	return wf, nil
}

// ListWorkflows lists all workflows
func (lc *LocalController) ListWorkflows() []*wfv1.Workflow {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	workflows := make([]*wfv1.Workflow, 0, len(lc.workflows))
	for _, wfExec := range lc.workflows {
		wfExec.mu.RLock()
		wf := wfExec.Workflow.DeepCopy()
		wf.Status = *wfExec.Status
		wfExec.mu.RUnlock()
		workflows = append(workflows, wf)
	}

	return workflows
}

// Close closes the local controller and cleans up resources
func (lc *LocalController) Close() error {
	return lc.executor.Close()
}
