package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acmestack/gpi/internal/backend"
	"github.com/acmestack/gpi/internal/state"
	"github.com/acmestack/gpi/internal/task"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	store, err := state.OpenAt(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	mgr, err := backend.New(store, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(store, mgr)
}

func TestRawEncoder(t *testing.T) {
	enc := NewResponseEncoder("raw")
	out := enc.EncodeSuccess(200, map[string]string{"a": "b"})
	b, _ := json.Marshal(out)
	if string(b) != `{"a":"b"}` {
		t.Fatalf("raw success = %s", b)
	}
	e := enc.EncodeError(404, errors.New("boom"))
	b, _ = json.Marshal(e)
	if string(b) != `{"error":"boom"}` {
		t.Fatalf("raw error = %s", b)
	}
}

func TestEnvelopeEncoder(t *testing.T) {
	enc := NewEnvelopeEncoder(EnvelopeConfig{})
	out := enc.EncodeSuccess(200, "hi")
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("envelope success not a map: %T", out)
	}
	if m["code"] != 200 || m["message"] != "ok" || m["data"] != "hi" {
		t.Fatalf("envelope success = %v", m)
	}
	e := enc.EncodeError(500, errors.New("oops"))
	m, ok = e.(map[string]any)
	if !ok {
		t.Fatalf("envelope error not a map: %T", e)
	}
	if m["code"] != 500 || m["message"] != "oops" {
		t.Fatalf("envelope error = %v", m)
	}
}

func TestEnvelopeCustomFields(t *testing.T) {
	enc := NewEnvelopeEncoder(EnvelopeConfig{Code: "status", Message: "msg", Data: "payload"})
	out := enc.EncodeSuccess(201, 7)
	m := out.(map[string]any)
	if m["status"] != 201 || m["msg"] != "ok" || m["payload"] != 7 {
		t.Fatalf("custom envelope = %v", m)
	}
}

func TestServerEnvelopeResponse(t *testing.T) {
	s := testServer(t)
	s.SetResponseEncoder(NewEnvelopeEncoder(EnvelopeConfig{}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/clusters", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != float64(200) || body["message"] != "ok" {
		t.Fatalf("envelope body = %v", body)
	}
	if _, ok := body["data"]; !ok {
		t.Fatalf("envelope missing data: %v", body)
	}
}

func TestServerRawResponse(t *testing.T) {
	s := testServer(t)
	s.SetResponseEncoder(NewResponseEncoder("raw"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/clusters", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("raw body should be an object with data, got: %s", rec.Body.String())
	}
	if _, ok := body["data"]; !ok {
		t.Fatalf("raw body missing data wrapper: %v", body)
	}
	if _, ok := body["requestId"]; !ok {
		t.Fatalf("raw body missing request_id: %v", body)
	}
}

func TestCustomEncoder(t *testing.T) {
	// A team can supply their own encoder for arbitrary response shapes.
	s := testServer(t)
	s.SetResponseEncoder(customEncoder{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/clusters", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("custom body = %s", rec.Body.String())
	}
	if body["status"] != "success" {
		t.Fatalf("custom response = %v", body)
	}
	if _, ok := body["requestId"]; !ok {
		t.Fatalf("custom response missing request_id: %v", body)
	}
	if _, ok := body["result"]; !ok {
		t.Fatalf("custom response missing result: %v", body)
	}
}

type customEncoder struct{}

func (customEncoder) EncodeSuccess(_ int, data any) any {
	return map[string]any{"status": "success", "result": data}
}

func (customEncoder) EncodeError(status int, err error) any {
	return map[string]any{"status": "error", "code": status, "message": err.Error()}
}

func TestRequestIDGenerated(t *testing.T) {
	s := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/clusters", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	id := rec.Header().Get(DefaultRequestIDHeader)
	if len(id) != 32 {
		t.Fatalf("generated request id = %q, want 32 hex chars", id)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["requestId"] != id {
		t.Fatalf("body request_id = %v, header = %q", body["requestId"], id)
	}
}

func TestRequestIDPassthrough(t *testing.T) {
	s := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/clusters", nil)
	req.Header.Set(DefaultRequestIDHeader, "upstream-trace-123")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if got := rec.Header().Get(DefaultRequestIDHeader); got != "upstream-trace-123" {
		t.Fatalf("header = %q, want passthrough", got)
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["requestId"] != "upstream-trace-123" {
		t.Fatalf("body request_id = %v, want passthrough", body["requestId"])
	}
}

func TestRequestIDErrorBody(t *testing.T) {
	s := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/clusters/missing", nil)
	req.Header.Set(DefaultRequestIDHeader, "err-trace")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["requestId"] != "err-trace" {
		t.Fatalf("error body request_id = %v", body["requestId"])
	}
	if body["error"] != "cluster not found" {
		t.Fatalf("error body = %v", body)
	}
}

func TestRequestIDCustomHeaderKey(t *testing.T) {
	s := testServer(t)
	s.SetRequestIDHeader("X-Trace-Id")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/clusters", nil)
	req.Header.Set("X-Trace-Id", "my-trace")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Trace-Id"); got != "my-trace" {
		t.Fatalf("custom header = %q", got)
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["requestId"] != "my-trace" {
		t.Fatalf("body request_id = %v", body["requestId"])
	}
}

func TestRequestIDInEnvelope(t *testing.T) {
	s := testServer(t)
	s.SetResponseEncoder(NewEnvelopeEncoder(EnvelopeConfig{}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/clusters", nil)
	req.Header.Set(DefaultRequestIDHeader, "env-trace")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != float64(200) || body["requestId"] != "env-trace" {
		t.Fatalf("envelope body = %v", body)
	}
}

func TestValidateHeaderKey(t *testing.T) {
	if err := validateHeaderKey("x-request-id"); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	if err := validateHeaderKey("X-Trace-Id"); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	if err := validateHeaderKey(""); err == nil {
		t.Fatal("empty key should fail")
	}
	if err := validateHeaderKey("x:requestid"); err == nil {
		t.Fatal("colon key should fail")
	}
	if err := validateHeaderKey("x request"); err == nil {
		t.Fatal("space key should fail")
	}
}

func TestTokenLifecycleAPI(t *testing.T) {
	s := testServer(t)

	// Create token.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gpi/tokens", strings.NewReader(`{"name":"ci","creator":"qicz"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	json.Unmarshal(rec.Body.Bytes(), &created)
	secret := created["token"].(string)
	tokenID := created["tokenId"].(string)
	if secret == "" {
		t.Fatal("no secret returned")
	}

	// List includes the token (no secret).
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/tokens", nil)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec2.Code)
	}

	// Rotate invalidates old secret.
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/gpi/tokens/"+tokenID+"/rotate", strings.NewReader(`{}`))
	req3.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("rotate status = %d: %s", rec3.Code, rec3.Body.String())
	}

	// Delete.
	req4 := httptest.NewRequest(http.MethodDelete, "/api/v1/gpi/tokens/"+tokenID, nil)
	rec4 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec4.Code)
	}
}

func TestAuthRequired(t *testing.T) {
	s := testServer(t)
	s.AuthRequired = true

	// Without token -> 401.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/clusters", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token status = %d", rec.Code)
	}

	// Create a token (public endpoint).
	_, secret, err := s.Store.CreateToken("ci", "qicz", 0)
	if err != nil {
		t.Fatal(err)
	}

	// Valid token -> 200.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/clusters", nil)
	req2.Header.Set("Authorization", "Bearer "+secret)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("valid token status = %d: %s", rec2.Code, rec2.Body.String())
	}

	// Wrong token -> 401.
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/clusters", nil)
	req3.Header.Set("Authorization", "Bearer wrong-token")
	rec3 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d", rec3.Code)
	}

	// healthz is public.
	req4 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec4 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", rec4.Code)
	}
}

func TestConfigAPI(t *testing.T) {
	s := testServer(t)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/gpi/config/autostop", strings.NewReader(`{"value":"true"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set config status = %d", rec.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/config/autostop", nil)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	var body map[string]string
	json.Unmarshal(rec2.Body.Bytes(), &body)
	if body["value"] != "true" {
		t.Fatalf("config value = %q", body["value"])
	}
}

func TestCORSMiddleware(t *testing.T) {
	s := testServer(t)
	s.EnableCORS = true

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/gpi/clusters", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS origin = %q", got)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("security header = %q", got)
	}
}

func TestCustomMiddleware(t *testing.T) {
	s := testServer(t)
	s.AddMiddleware(MiddlewareFunc(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Custom-Middleware", "invoked")
			next.ServeHTTP(w, r)
		})
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Custom-Middleware"); got != "invoked" {
		t.Fatalf("custom middleware not invoked: %q", got)
	}
}

func TestDocsEndpoints(t *testing.T) {
	s := testServer(t)
	s.EnableDocs = true

	// Swagger spec is valid JSON with openapi key.
	req := httptest.NewRequest(http.MethodGet, "/swagger.json", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("swagger status = %d", rec.Code)
	}
	var spec map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("swagger not JSON: %v", err)
	}
	if spec["openapi"] == "" {
		t.Fatalf("swagger missing openapi key: %v", spec)
	}
	if spec["paths"] == nil {
		t.Fatal("swagger missing paths")
	}

	// Docs UI serves HTML.
	req2 := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK || !strings.Contains(rec2.Body.String(), "swagger-ui") {
		t.Fatalf("docs status = %d body = %s", rec2.Code, rec2.Body.String())
	}

	// Docs disabled -> 404.
	s2 := testServer(t)
	req3 := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec3 := httptest.NewRecorder()
	s2.Handler().ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("docs disabled status = %d", rec3.Code)
	}
}

func TestKeyStyleToCase(t *testing.T) {
	cases := []struct {
		in, style, want string
	}{
		{"num_nodes", "camel", "numNodes"},
		{"num_nodes", "snake", "num_nodes"},
		{"num_nodes", "pascal", "NumNodes"},
		{"numNodes", "snake", "num_nodes"},
		{"NumNodes", "snake", "num_nodes"},
		{"numNodes", "pascal", "NumNodes"},
		{"request_id", "camel", "requestId"},
		{"request_id", "pascal", "RequestId"},
		{"cluster_name", "camel", "clusterName"},
		{"instanceType", "snake", "instance_type"},
		{"CPU", "snake", "cpu"},
		{"gpu_count", "pascal", "GpuCount"},
		{"data", "camel", "data"},
		{"a_b_c", "camel", "aBC"},
	}
	for _, c := range cases {
		got := toCase(c.in, KeyStyle(c.style))
		if got != c.want {
			t.Errorf("toCase(%q, %s) = %q, want %q", c.in, c.style, got, c.want)
		}
	}
}

func TestApplyKeyStyle(t *testing.T) {
	in := map[string]any{
		"num_nodes":    2,
		"cluster_name": "demo",
		"instances": []any{
			map[string]any{"public_ip": "1.2.3.4", "instance_type": "p4d"},
		},
	}
	snake := applyKeyStyle(in, KeyStyleSnake).(map[string]any)
	if snake["num_nodes"] != float64(2) || snake["cluster_name"] != "demo" {
		t.Fatalf("snake = %v", snake)
	}
	camel := applyKeyStyle(in, KeyStyleCamel).(map[string]any)
	if camel["numNodes"] != float64(2) || camel["clusterName"] != "demo" {
		t.Fatalf("camel = %v", camel)
	}
	if _, ok := camel["num_nodes"]; ok {
		t.Fatalf("camel should not keep num_nodes: %v", camel)
	}
	arr := camel["instances"].([]any)[0].(map[string]any)
	if arr["publicIp"] != "1.2.3.4" {
		t.Fatalf("nested camel = %v", arr)
	}
	pascal := applyKeyStyle(in, KeyStylePascal).(map[string]any)
	if pascal["NumNodes"] != float64(2) || pascal["ClusterName"] != "demo" {
		t.Fatalf("pascal = %v", pascal)
	}
}

func TestApplyKeyStyleRewritesSchemaRef(t *testing.T) {
	// OpenAPI $ref pointers must follow the converted component key so schema
	// references stay resolvable under any key style. Test the string-rewrite
	// rule in isolation (surrounding keys are style-converted separately).
	refIn := map[string]any{
		"$ref": "#/components/schemas/launchRequest",
	}
	for style, want := range map[KeyStyle]string{
		KeyStyleCamel:  "#/components/schemas/launchRequest",
		KeyStyleSnake:  "#/components/schemas/launch_request",
		KeyStylePascal: "#/components/schemas/LaunchRequest",
	} {
		out := applyKeyStyle(refIn, style).(map[string]any)
		if got := out["$ref"].(string); got != want {
			t.Fatalf("%s: ref = %q, want %q", style, got, want)
		}
	}
}

func TestServerKeyStyle(t *testing.T) {
	s := testServer(t)

	// Create a cluster so /api/v1/gpi/clusters returns real data with snake tags.
	store := s.Store
	if err := store.AddCluster(&state.Cluster{
		Name: "demo", Cloud: "aws", Region: "us-east-1", NumNodes: 2,
		Instances: []state.Node{{ID: "i-1", Role: state.RoleHead, PublicIP: "1.2.3.4"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Default is camel.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/clusters", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	arr := body["data"].([]any)
	cl := arr[0].(map[string]any)
	if cl["numNodes"] != float64(2) || cl["cloud"] != "aws" {
		t.Fatalf("camel body = %v", cl)
	}
	if _, ok := cl["num_nodes"]; ok {
		t.Fatalf("should be camel, not snake: %v", cl)
	}

	// Switch to snake.
	s.SetKeyStyle(KeyStyleSnake)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req)
	var body2 map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &body2)
	arr2 := body2["data"].([]any)
	if arr2[0].(map[string]any)["num_nodes"] != float64(2) {
		t.Fatalf("snake body = %v", arr2[0])
	}
	if _, ok := arr2[0].(map[string]any)["numNodes"]; ok {
		t.Fatalf("should be snake, not camel: %v", arr2[0])
	}
}

func TestServerKeyStyleWithRequestID(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpi/clusters", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	// Default camel: request_id field rendered as requestId.
	if _, ok := body["requestId"]; !ok {
		t.Fatalf("expected requestId in camel body: %v", body)
	}
	if _, ok := body["request_id"]; ok {
		t.Fatalf("should not keep request_id in camel: %v", body)
	}
}

// TestLaunchTaskJSONBody verifies POST /api/v1/gpi/tasks/{name}/launch accepts a
// structured Task JSON body (backend=local to avoid any cloud calls) and that
// the cluster is created with the backend resolved from the JSON body.
func TestLaunchTaskJSONBody(t *testing.T) {
	s := testServer(t)
	body := `{"task":{"name":"j1","backend":"local","num_nodes":1,"setup":"echo setup","run":"echo hi"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gpi/tasks/j1/launch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	cl, err := s.Store.GetCluster("j1")
	if err != nil {
		t.Fatal(err)
	}
	if cl.Backend != task.BackendLocal {
		t.Fatalf("backend = %q, want %q", cl.Backend, task.BackendLocal)
	}
}

// TestLaunchTaskJSONBodyRejectsMissingTask verifies the task object is required.
func TestLaunchTaskJSONBodyRejectsMissingTask(t *testing.T) {
	s := testServer(t)
	body := `{"cloud":"aliyun"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gpi/tasks/j2/launch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
}
