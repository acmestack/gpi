package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/acmestack/gpi/internal/backend"
	"github.com/acmestack/gpi/internal/jobs"
	"github.com/acmestack/gpi/internal/optimizer"
	"github.com/acmestack/gpi/internal/serve"
	"github.com/acmestack/gpi/internal/state"
	"github.com/acmestack/gpi/internal/task"
)

// Server exposes the gpi control plane as a REST API plus a background
// job scheduler.
type Server struct {
	Store    *state.Store
	Prov     *backend.Manager
	Serve    *serve.Manager
	Jobs     *jobs.Manager
	Log      *log.Logger
	Response ResponseEncoder
	// RequestIDHeader is the header key carrying the request ID (default
	// "x-request-id", configurable via GPI_REQUEST_ID_HEADER).
	RequestIDHeader string
	// RequestIDBodyField is the JSON field injected into response bodies
	// (default "request_id").
	RequestIDBodyField string
	// KeyStyle is the case style applied to every JSON key in responses
	// (default camel, configurable via GPI_API_RESPONSE_KEY_STYLE).
	KeyStyle KeyStyle
	// AuthRequired enables bearer-token authentication on the API.
	AuthRequired bool
	// EnableCORS adds permissive CORS headers to responses.
	EnableCORS bool
	// EnableDocs exposes the OpenAPI spec and Swagger UI.
	EnableDocs bool
	// APIPrefix is the URL prefix for all REST routes (default
	// "/api/v1/gpi", configurable via GPI_API_PREFIX or SetAPIPrefix).
	APIPrefix string

	// extraMiddlewares are user-registered middlewares appended after the
	// built-in chain (outermost last).
	extraMiddlewares []Middleware
}

// DefaultAPIPrefix is the default URL prefix for all REST routes.
const DefaultAPIPrefix = "/api/v1/gpi"

// New builds a Server bound to the given store and execution backend.
// The response encoder defaults to GPI_RESPONSE_FORMAT (raw|envelope), the
// request ID header to GPI_REQUEST_ID_HEADER (x-request-id), the key style
// to GPI_API_RESPONSE_KEY_STYLE (camel) and the API prefix to GPI_API_PREFIX
// (/api/v1/gpi).
func New(store *state.Store, prov *backend.Manager) *Server {
	reqHeader := envOr("GPI_REQUEST_ID_HEADER", DefaultRequestIDHeader)
	prefix := envOr("GPI_API_PREFIX", DefaultAPIPrefix)
	return &Server{
		Store:              store,
		Prov:               prov,
		Serve:              serve.New(store, prov),
		Jobs:               jobs.New(store, prov),
		Log:                log.New(os.Stderr, "[gpi-server] ", log.LstdFlags),
		Response:           responseFormatFromEnv(),
		RequestIDHeader:    reqHeader,
		RequestIDBodyField: DefaultRequestIDBodyField,
		KeyStyle:           keyStyleFromEnv(),
		APIPrefix:          prefix,
	}
}

// SetAPIPrefix overrides the URL prefix for all REST routes.
func (s *Server) SetAPIPrefix(prefix string) {
	if prefix == "" {
		return
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	s.APIPrefix = strings.TrimRight(prefix, "/")
}

// AddMiddleware appends a custom middleware to the request chain. Custom
// middlewares run outermost (first), so they can observe/short-circuit before
// the built-in auth and logging layers.
func (s *Server) AddMiddleware(m Middleware) {
	if m != nil {
		s.extraMiddlewares = append(s.extraMiddlewares, m)
	}
}

// SetResponseEncoder overrides the response wrapping used by all handlers.
func (s *Server) SetResponseEncoder(enc ResponseEncoder) {
	if enc != nil {
		s.Response = enc
	}
}

// SetRequestIDHeader overrides the header key used to carry the request ID.
func (s *Server) SetRequestIDHeader(key string) {
	if err := validateHeaderKey(key); err == nil {
		s.RequestIDHeader = key
	}
}

// SetKeyStyle overrides the JSON key case style used in responses.
func (s *Server) SetKeyStyle(style KeyStyle) {
	for _, v := range ValidKeyStyles {
		if style == v {
			s.KeyStyle = style
			return
		}
	}
}

// Handler builds the HTTP handler with all routes and middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	api := s.APIPrefix
	if api == "" {
		api = DefaultAPIPrefix
	}
	if !strings.HasPrefix(api, "/") {
		api = "/" + api
	}

	mux.HandleFunc("GET "+api+"/clusters", s.handleListClusters)
	mux.HandleFunc("GET "+api+"/clusters/{name}", s.handleGetCluster)
	mux.HandleFunc("POST "+api+"/clusters/{name}/launch", s.handleLaunch)
	mux.HandleFunc("DELETE "+api+"/clusters/{name}", s.handleDeleteCluster)

	// tasks/{name}/launch accepts the task as a structured JSON body (the
	// "task" field is a Task object), distinct from clusters/{name}/launch
	// which takes a task YAML string.
	mux.HandleFunc("POST "+api+"/tasks/{name}/launch", s.handleLaunchTask)

	mux.HandleFunc("GET "+api+"/services", s.handleListServices)
	mux.HandleFunc("POST "+api+"/services/up", s.handleServiceUp)
	mux.HandleFunc("DELETE "+api+"/services/{name}", s.handleServiceDown)

	mux.HandleFunc("GET "+api+"/jobs", s.handleListJobs)
	mux.HandleFunc("POST "+api+"/jobs", s.handleSubmitJob)
	mux.HandleFunc("POST "+api+"/jobs/{name}/run", s.handleRunJob)

	mux.HandleFunc("GET "+api+"/config", s.handleListConfig)
	mux.HandleFunc("GET "+api+"/config/{key}", s.handleGetConfig)
	mux.HandleFunc("PUT "+api+"/config/{key}", s.handleSetConfig)

	mux.HandleFunc("POST "+api+"/tokens", s.handleCreateToken)
	mux.HandleFunc("GET "+api+"/tokens", s.handleListTokens)
	mux.HandleFunc("DELETE "+api+"/tokens/{id}", s.handleDeleteToken)
	mux.HandleFunc("POST "+api+"/tokens/{id}/rotate", s.handleRotateToken)

	if s.EnableDocs {
		mux.HandleFunc("GET /swagger.json", s.handleSwaggerSpec)
		mux.HandleFunc("GET /docs", s.handleDocsUI)
		mux.HandleFunc("GET /redoc", s.handleRedocUI)
	}

	return chain(mux, s.middlewares())
}

// middlewares assembles the ordered middleware chain, outermost first.
// Custom middlewares run first (outermost), then security headers, CORS,
// auth, request-id and logging (innermost).
func (s *Server) middlewares() []Middleware {
	mws := []Middleware{}
	mws = append(mws, s.extraMiddlewares...)
	mws = append(mws, &securityHeadersMiddleware{})
	if s.EnableCORS {
		mws = append(mws, newCORSMiddleware(nil))
	}
	mws = append(mws, newAuthMiddleware(s.Store, s.AuthRequired))
	mws = append(mws, requestIDMiddleware{headerKey: s.RequestIDHeader})
	mws = append(mws, &loggingMiddleware{logf: s.Log.Printf})
	return mws
}

// Run starts the HTTP server on the given port, also driving the background
// job scheduler, and blocks until ctx is cancelled or the server fails.
func (s *Server) Run(ctx context.Context, port int) error {
	go s.scheduler(ctx)
	srv := &http.Server{
		Addr:    ":" + strconv.Itoa(port),
		Handler: s.Handler(),
	}
	s.Log.Printf("listening on %s", srv.Addr)
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Server) scheduler(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, job := range s.Jobs.Due() {
				jobCopy := job
				go func() {
					s.Log.Printf("running scheduled job %s", jobCopy.Name)
					err := s.Jobs.RunNow(ctx, jobCopy.Name, nil)
					if err != nil {
						s.Log.Printf("job %s failed: %v", jobCopy.Name, err)
					}
					s.Jobs.Reschedule(jobCopy)
				}()
			}
		}
	}
}

type launchRequest struct {
	Name     string `json:"name"`
	TaskYaml string `json:"task"`
	Cluster  string `json:"clusterName"`
	Cloud    string `json:"cloud"`
	Region   string `json:"region"`
	NumNodes int    `json:"numNodes"`
	UseSpot  bool   `json:"useSpot"`
	DryRun   bool   `json:"dryRun"`
	RunTask  bool   `json:"runTask"`
	// Optimizer selects the placement optimizer or strategy, e.g. "cost",
	// "time", or "cost,time". Empty defaults to "cost".
	Optimizer string `json:"optimizer"`
}

func (s *Server) handleListClusters(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.Store.ListClusters())
}

func (s *Server) handleGetCluster(w http.ResponseWriter, r *http.Request) {
	cluster, err := s.Store.GetCluster(r.PathValue("name"))
	if err != nil {
		s.writeError(w, http.StatusNotFound, err)
		return
	}
	s.writeJSON(w, http.StatusOK, cluster)
}

func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	var req launchRequest
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	ts, err := task.Parse([]byte(req.TaskYaml))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("invalid task: %w", err))
		return
	}
	name := req.Cluster
	if name == "" {
		name = req.Name
	}
	if name == "" {
		name = ts.Name
	}
	if req.NumNodes > 0 {
		ts.NumNodes = req.NumNodes
	}
	s.launchTask(w, r, name, ts, req.Cloud, req.Region, req.UseSpot, req.DryRun, req.RunTask, req.Optimizer)
}

func (s *Server) handleDeleteCluster(w http.ResponseWriter, r *http.Request) {
	if err := s.Prov.Down(r.Context(), r.PathValue("name")); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// taskLaunchRequest accepts a task as a structured JSON body (task field is a
// Task object), unlike launchRequest which takes a task YAML string.
type taskLaunchRequest struct {
	Task     *task.Task `json:"task"`
	Cloud    string     `json:"cloud"`
	Region   string     `json:"region"`
	NumNodes int        `json:"numNodes"`
	UseSpot  bool       `json:"useSpot"`
	DryRun   bool       `json:"dryRun"`
	RunTask  bool       `json:"runTask"`
	// Optimizer selects the placement optimizer or strategy, e.g. "cost",
	// "time", or "cost,time". Empty defaults to "cost".
	Optimizer string `json:"optimizer"`
}

// handleLaunchTask launches a task supplied as a structured JSON Task body.
func (s *Server) handleLaunchTask(w http.ResponseWriter, r *http.Request) {
	var req taskLaunchRequest
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Task == nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("task is required"))
		return
	}
	ts := req.Task
	name := r.PathValue("name")
	if name == "" {
		name = ts.Name
	}
	if req.NumNodes > 0 {
		ts.NumNodes = req.NumNodes
	}
	if err := ts.SetDefaults(); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	// The path name wins over SetDefaults' "task" default.
	if name != "" {
		ts.Name = name
	}
	s.launchTask(w, r, name, ts, req.Cloud, req.Region, req.UseSpot, req.DryRun, req.RunTask, req.Optimizer)
}

// launchTask runs the shared launch pipeline: optimize placement (unless a
// non-cloud backend is used), provision, and optionally start running.
func (s *Server) launchTask(w http.ResponseWriter, r *http.Request, name string, ts *task.Task, cloud, region string, useSpot, dryRun, runTask bool, optimizerName string) {
	var launch *optimizer.Launch
	if ts.Backend != task.BackendCloud {
		launch = &optimizer.Launch{Cloud: ts.Backend, NumNodes: ts.NumNodes}
	} else {
		opt, err := optimizer.Resolve(optimizerName)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}
		plan, err := opt.Optimize(r.Context(), &optimizer.Request{
			Resources: ts.Resources,
			Options: &optimizer.Options{
				NumNodes: ts.NumNodes,
				Cloud:    cloud,
				Region:   region,
				UseSpot:  useSpot,
			},
		})
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}
		if dryRun {
			s.writeJSON(w, http.StatusOK, plan)
			return
		}
		launch = plan.Launches[0]
	}
	cluster, err := s.Prov.Launch(r.Context(), name, ts, launch)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if runTask {
		go func() {
			s.Prov.RunTask(context.Background(), cluster.Name, ts, nil)
		}()
	}
	s.writeJSON(w, http.StatusOK, cluster)
}

func (s *Server) handleListServices(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.Store.ListServices())
}

type serviceUpRequest struct {
	Name     string `json:"name"`
	TaskYaml string `json:"task"`
	Cloud    string `json:"cloud"`
	Region   string `json:"region"`
	// Optimizer selects the placement optimizer or strategy; empty = "cost".
	Optimizer string `json:"optimizer"`
}

func (s *Server) handleServiceUp(w http.ResponseWriter, r *http.Request) {
	var req serviceUpRequest
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	ts, err := task.Parse([]byte(req.TaskYaml))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("invalid task: %w", err))
		return
	}
	opt, err := optimizer.Resolve(req.Optimizer)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	plan, err := opt.Optimize(r.Context(), &optimizer.Request{
		Resources: ts.Resources,
		Options: &optimizer.Options{
			Cloud:  req.Cloud,
			Region: req.Region,
		},
	})
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	name := req.Name
	if name == "" {
		name = ts.Name
	}
	svc, err := s.Serve.Up(r.Context(), name, ts, plan)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, http.StatusOK, svc)
}

func (s *Server) handleServiceDown(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	svc, err := s.Store.GetService(name)
	if err != nil {
		s.writeError(w, http.StatusNotFound, err)
		return
	}
	for _, clusterName := range svc.ReplicaClusters {
		s.Prov.Down(r.Context(), clusterName)
	}
	s.Store.DeleteService(name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListJobs(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.Store.ListJobs())
}

type jobSubmitRequest struct {
	Name      string `json:"name"`
	TaskPath  string `json:"task_path"`
	TaskYaml  string `json:"task"`
	Schedule  string `json:"schedule"`
	Retries   int    `json:"retries"`
	RunNow    bool   `json:"run_now"`
	Optimizer string `json:"optimizer"`
}

func (s *Server) handleSubmitJob(w http.ResponseWriter, r *http.Request) {
	var req jobSubmitRequest
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	taskPath := req.TaskPath
	if taskPath == "" && req.TaskYaml != "" {
		tmp, err := writeTempTask(req.TaskYaml)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		taskPath = tmp
	}
	if taskPath == "" {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("task_path or task is required"))
		return
	}
	job, err := s.Jobs.Submit(req.Name, taskPath, req.Schedule, req.Retries, req.Optimizer)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.RunNow {
		go func() {
			s.Jobs.RunNow(r.Context(), job.Name, nil)
		}()
	}
	s.writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleRunJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.Store.GetJob(r.PathValue("name"))
	if err != nil {
		s.writeError(w, http.StatusNotFound, err)
		return
	}
	if job.Status == "running" {
		s.writeError(w, http.StatusConflict, fmt.Errorf("job %s is already running", job.Name))
		return
	}
	go func() {
		err := s.Jobs.RunNow(context.Background(), job.Name, nil)
		if err != nil {
			s.Log.Printf("job %s failed: %v", job.Name, err)
		}
	}()
	s.writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func decodeJSON(r *http.Request, out any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (s *Server) writeJSON(w http.ResponseWriter, code int, data any) {
	var body any = data
	if s.Response != nil {
		body = s.Response.EncodeSuccess(code, data)
	}
	if id := requestIDOf(w); id != "" {
		body = injectRequestID(body, id, s.RequestIDBodyField)
	}
	writeRawJSON(w, code, applyKeyStyle(body, s.KeyStyle))
}

func (s *Server) writeError(w http.ResponseWriter, code int, err error) {
	var body any = map[string]string{"error": err.Error()}
	if s.Response != nil {
		body = s.Response.EncodeError(code, err)
	}
	if id := requestIDOf(w); id != "" {
		body = injectRequestID(body, id, s.RequestIDBodyField)
	}
	writeRawJSON(w, code, applyKeyStyle(body, s.KeyStyle))
}

func writeRawJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

func writeTempTask(yamlStr string) (string, error) {
	dir, err := os.MkdirTemp("", "gpi-job-")
	if err != nil {
		return "", err
	}
	path := dir + "/task.yaml"
	if err := os.WriteFile(path, []byte(yamlStr), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
