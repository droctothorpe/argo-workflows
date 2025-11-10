package local

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/util/logging"
	"sigs.k8s.io/yaml"
)

// Server is the HTTP server for local workflow execution
type Server struct {
	controller *LocalController
	logger     logging.Logger
	port       int
}

// NewServer creates a new local execution server
func NewServer(ctx context.Context, port int) (*Server, error) {
	controller, err := NewLocalController(ctx)
	if err != nil {
		return nil, err
	}

	return &Server{
		controller: controller,
		logger:     logging.RequireLoggerFromContext(ctx),
		port:       port,
	}, nil
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Register handlers
	mux.HandleFunc("/api/v1/workflows", s.handleWorkflows)
	mux.HandleFunc("/api/v1/workflows/", s.handleWorkflowDetail)
	mux.HandleFunc("/healthz", s.handleHealth)

	addr := fmt.Sprintf(":%d", s.port)
	s.logger.WithField("address", addr).Info(ctx, "Starting local execution server")

	server := &http.Server{
		Addr:    addr,
		Handler: s.loggingMiddleware(mux),
	}

	// Start server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.WithError(err).Error(ctx, "Server error")
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	s.logger.Info(ctx, "Shutting down server")
	return server.Shutdown(context.Background())
}

// handleWorkflows handles workflow list and submission
func (s *Server) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		s.listWorkflows(ctx, w, r)
	case http.MethodPost:
		s.submitWorkflow(ctx, w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleWorkflowDetail handles individual workflow operations
func (s *Server) handleWorkflowDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract workflow name from path
	name := r.URL.Path[len("/api/v1/workflows/"):]
	if name == "" {
		http.Error(w, "Workflow name required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getWorkflow(ctx, w, r, name)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listWorkflows lists all workflows
func (s *Server) listWorkflows(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	workflows := s.controller.ListWorkflows()

	response := map[string]interface{}{
		"items": workflows,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.WithError(err).Error(ctx, "Failed to encode response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// getWorkflow retrieves a specific workflow
func (s *Server) getWorkflow(ctx context.Context, w http.ResponseWriter, r *http.Request, name string) {
	workflow, err := s.controller.GetWorkflow(name)
	if err != nil {
		s.logger.WithError(err).WithField("workflow", name).Warn(ctx, "Workflow not found")
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(workflow); err != nil {
		s.logger.WithError(err).Error(ctx, "Failed to encode response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// submitWorkflow submits a new workflow for execution
func (s *Server) submitWorkflow(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.WithError(err).Error(ctx, "Failed to read request body")
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse workflow from YAML or JSON
	var workflow wfv1.Workflow
	
	// Try JSON first
	if err := json.Unmarshal(body, &workflow); err != nil {
		// Try YAML
		if err := yaml.Unmarshal(body, &workflow); err != nil {
			s.logger.WithError(err).Error(ctx, "Failed to parse workflow")
			http.Error(w, "Failed to parse workflow: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Validate workflow has required fields
	if workflow.Name == "" {
		if workflow.ObjectMeta.Name != "" {
			workflow.Name = workflow.ObjectMeta.Name
		} else {
			http.Error(w, "Workflow name is required", http.StatusBadRequest)
			return
		}
	}

	if workflow.Spec.Entrypoint == "" {
		http.Error(w, "Workflow entrypoint is required", http.StatusBadRequest)
		return
	}

	// Submit workflow
	if err := s.controller.SubmitWorkflow(ctx, &workflow); err != nil {
		s.logger.WithError(err).Error(ctx, "Failed to submit workflow")
		http.Error(w, "Failed to submit workflow: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.logger.WithField("workflow", workflow.Name).Info(ctx, "Workflow submitted")

	// Return the workflow
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(workflow); err != nil {
		s.logger.WithError(err).Error(ctx, "Failed to encode response")
	}
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}

// loggingMiddleware logs HTTP requests
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		s.logger.WithFields(logging.Fields{
			"method": r.Method,
			"path":   r.URL.Path,
			"remote": r.RemoteAddr,
		}).Debug(ctx, "HTTP request")
		next.ServeHTTP(w, r)
	})
}

// Close closes the server and cleans up resources
func (s *Server) Close() error {
	return s.controller.Close()
}
