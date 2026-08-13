package server

import (
	"encoding/json"
	"net/http"
)

// OpenAPISpecJSON renders the OpenAPI 3.0 document (with the given API prefix)
// as indented JSON, used to regenerate docs/openapi.json.
func OpenAPISpecJSON(prefix string) ([]byte, error) {
	return json.MarshalIndent(swaggerSpec(prefix), "", "  ")
}

// swaggerSpec returns the OpenAPI 3.0 document describing the gpi API. Paths
// include the configured API prefix (e.g. /api/v1/gpi) so the spec matches the
// actual server routes exactly.
func swaggerSpec(prefix string) map[string]any {
	jsonResp := map[string]any{
		"description": "Success",
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": map[string]any{"type": "object"},
			},
		},
	}
	errorResp := map[string]any{
		"description": "Error",
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": map[string]any{"type": "object"},
			},
		},
	}
	path := func(method, summary string, tags []string) map[string]any {
		return map[string]any{
			"summary": summary,
			"tags":    tags,
			"responses": map[string]any{
				"200": jsonResp,
				"400": errorResp,
				"500": errorResp,
			},
		}
	}
	// post adds a JSON request body schema to a POST operation.
	post := func(summary string, tags []string, schema string) map[string]any {
		p := path("post", summary, tags)
		p["requestBody"] = map[string]any{
			"required": true,
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": map[string]any{"$ref": "#/components/schemas/" + schema},
				},
			},
		}
		return p
	}
	authBearer := map[string]any{
		"type":         "http",
		"scheme":       "bearer",
		"bearerFormat": "token",
	}
	// p prefixes a path with the API prefix so the spec shows full routes.
	p := func(p string) string {
		return prefix + p
	}
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":   "gpi API",
			"version": "0.1.0",
			"description": "gpi multi-cloud compute scheduling REST API. " +
				"Authenticate with `Authorization: Bearer <token>` when the server runs with --require-auth.",
		},
		"tags": []any{
			map[string]any{"name": "clusters", "description": "Cluster lifecycle"},
			map[string]any{"name": "tasks", "description": "Task submission (structured JSON body)"},
			map[string]any{"name": "services", "description": "Replicated services"},
			map[string]any{"name": "jobs", "description": "Scheduled jobs"},
			map[string]any{"name": "config", "description": "Runtime configuration"},
			map[string]any{"name": "tokens", "description": "Service-account tokens"},
		},
		"paths": map[string]any{
			p("/clusters"): map[string]any{
				"get": path("get", "List clusters", []string{"clusters"}),
			},
			p("/clusters/{name}"): map[string]any{
				"get":    path("get", "Get a cluster", []string{"clusters"}),
				"delete": path("delete", "Terminate a cluster", []string{"clusters"}),
				"parameters": []any{map[string]any{
					"name": "name", "in": "path", "required": true,
					"schema": map[string]any{"type": "string"},
				}},
			},
			p("/clusters/{name}/launch"): map[string]any{
				"post": post("Launch a task given as YAML string", []string{"clusters"}, "launchRequest"),
				"parameters": []any{map[string]any{
					"name": "name", "in": "path", "required": true,
					"schema": map[string]any{"type": "string"},
				}},
			},
			p("/tasks/{name}/launch"): map[string]any{
				"post": post("Launch a task given as a structured Task object", []string{"tasks"}, "taskLaunchRequest"),
				"parameters": []any{map[string]any{
					"name": "name", "in": "path", "required": true,
					"schema": map[string]any{"type": "string"},
				}},
			},
			p("/services"): map[string]any{
				"get": path("get", "List services", []string{"services"}),
			},
			p("/services/up"): map[string]any{
				"post": post("Deploy a service", []string{"services"}, "serviceUpRequest"),
			},
			p("/services/{name}"): map[string]any{
				"delete": path("delete", "Tear down a service", []string{"services"}),
				"parameters": []any{map[string]any{
					"name": "name", "in": "path", "required": true,
					"schema": map[string]any{"type": "string"},
				}},
			},
			p("/jobs"): map[string]any{
				"get":  path("get", "List jobs", []string{"jobs"}),
				"post": post("Submit a job", []string{"jobs"}, "jobSubmitRequest"),
			},
			p("/jobs/{name}/run"): map[string]any{
				"post": post("Run a job", []string{"jobs"}, "jobRunRequest"),
				"parameters": []any{map[string]any{
					"name": "name", "in": "path", "required": true,
					"schema": map[string]any{"type": "string"},
				}},
			},
			p("/config"): map[string]any{
				"get": path("get", "List config entries", []string{"config"}),
			},
			p("/config/{key}"): map[string]any{
				"get": path("get", "Get a config value", []string{"config"}),
				"put": path("put", "Set a config value", []string{"config"}),
				"parameters": []any{map[string]any{
					"name": "key", "in": "path", "required": true,
					"schema": map[string]any{"type": "string"},
				}},
			},
			p("/tokens"): map[string]any{
				"get":  path("get", "List tokens", []string{"tokens"}),
				"post": path("post", "Create a token", []string{"tokens"}),
			},
			p("/tokens/{id}"): map[string]any{
				"delete": path("delete", "Revoke a token", []string{"tokens"}),
				"parameters": []any{map[string]any{
					"name": "id", "in": "path", "required": true,
					"schema": map[string]any{"type": "string"},
				}},
			},
			p("/tokens/{id}/rotate"): map[string]any{
				"post": path("post", "Rotate a token", []string{"tokens"}),
				"parameters": []any{map[string]any{
					"name": "id", "in": "path", "required": true,
					"schema": map[string]any{"type": "string"},
				}},
			},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{"bearerAuth": authBearer},
			"schemas": map[string]any{
				"launchRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":        map[string]any{"type": "string", "description": "Cluster name (defaults to task name)"},
						"task":        map[string]any{"type": "string", "description": "Task YAML"},
						"clusterName": map[string]any{"type": "string"},
						"cloud":       map[string]any{"type": "string", "description": "Cloud filter"},
						"region":      map[string]any{"type": "string"},
						"numNodes":    map[string]any{"type": "integer"},
						"useSpot":     map[string]any{"type": "boolean"},
						"dryRun":      map[string]any{"type": "boolean", "description": "Only compute the placement plan"},
						"runTask":     map[string]any{"type": "boolean", "description": "Start running the task after provisioning"},
						"optimizer":   map[string]any{"type": "string", "description": "Placement optimizer or strategy: cost, time, or a priority list like cost,time (default: cost)"},
					},
				},
				"taskLaunchRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"task":      map[string]any{"$ref": "#/components/schemas/Task", "description": "Task object (structured JSON)"},
						"cloud":     map[string]any{"type": "string"},
						"region":    map[string]any{"type": "string"},
						"numNodes":  map[string]any{"type": "integer"},
						"useSpot":   map[string]any{"type": "boolean"},
						"dryRun":    map[string]any{"type": "boolean", "description": "Only compute the placement plan"},
						"runTask":   map[string]any{"type": "boolean", "description": "Start running the task after provisioning"},
						"optimizer": map[string]any{"type": "string", "description": "Placement optimizer or strategy (default: cost)"},
					},
					"required": []any{"task"},
				},
				"Task": map[string]any{
					"type":        "object",
					"description": "A task to launch: resources to provision plus setup/run commands.",
					"properties": map[string]any{
						"name":        map[string]any{"type": "string"},
						"numNodes":    map[string]any{"type": "integer", "description": "Number of nodes (default 1)"},
						"resources":   map[string]any{"$ref": "#/components/schemas/Resources"},
						"workdir":     map[string]any{"type": "string"},
						"fileMounts":  map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
						"tags":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
						"credentials": map[string]any{"type": "object"},
						"backend":     map[string]any{"type": "string", "description": "Execution backend: cloud|existing|docker|local (default cloud)"},
						"ssh":         map[string]any{"type": "object"},
						"docker":      map[string]any{"type": "object"},
						"setup":       map[string]any{"type": "string", "description": "Setup commands run on every node"},
						"run":         map[string]any{"type": "string", "description": "Run command on the head node"},
						"envs":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
						"time":        map[string]any{"type": "string"},
						"service":     map[string]any{"type": "object"},
					},
				},
				"Resources": map[string]any{
					"type":        "object",
					"description": "Compute requirements used to filter candidate instance types.",
					"properties": map[string]any{
						"cloud":        map[string]any{"type": "string"},
						"region":       map[string]any{"type": "string"},
						"zone":         map[string]any{"type": "string"},
						"instanceType": map[string]any{"type": "string"},
						"cpus":         map[string]any{"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}}, "description": "CPU range, e.g. \"8+\", \"4-8\", or a fixed number"},
						"memory":       map[string]any{"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}}, "description": "Memory in GiB, e.g. \"16+\""},
						"diskSize":     map[string]any{"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}}},
						"accelerators": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "integer"}, "description": "e.g. {\"A100\": 1}"},
						"useSpot":      map[string]any{"type": "boolean"},
						"timeSec":      map[string]any{"type": "integer", "description": "Estimated runtime in seconds (used by the time objective)"},
						"labels":       map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
					},
				},
				"serviceUpRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":      map[string]any{"type": "string"},
						"task":      map[string]any{"type": "string", "description": "Task YAML with a service block"},
						"cloud":     map[string]any{"type": "string"},
						"region":    map[string]any{"type": "string"},
						"optimizer": map[string]any{"type": "string", "description": "Placement optimizer or strategy (default: cost)"},
					},
				},
				"jobSubmitRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":     map[string]any{"type": "string"},
						"task":     map[string]any{"type": "string", "description": "Task YAML or task file path"},
						"schedule": map[string]any{"type": "string", "description": "Cron schedule, e.g. '0 0 * * *'"},
						"retries":  map[string]any{"type": "integer"},
					},
				},
				"jobRunRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
				},
			},
		},
		"security": []any{map[string]any{"bearerAuth": []string{}}},
	}
}

func (s *Server) handleSwaggerSpec(w http.ResponseWriter, _ *http.Request) {
	prefix := s.APIPrefix
	if prefix == "" {
		prefix = DefaultAPIPrefix
	}
	// The OpenAPI document is a spec, not business data: its keys follow the
	// OpenAPI 3.0 fixed schema (requestBody, responses, ...), so it must NOT go
	// through the response KeyStyle transform.
	writeRawJSON(w, http.StatusOK, swaggerSpec(prefix))
}

const docsHTML = `<!DOCTYPE html>
<html>
<head>
  <title>gpi API - Swagger UI</title>
  <meta charset="utf-8"/>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"/>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = () => SwaggerUIBundle({
      url: '/swagger.json',
      dom_id: '#swagger-ui',
      deepLinking: true,
    });
  </script>
</body>
</html>`

const redocHTML = `<!DOCTYPE html>
<html>
<head><title>gpi API - ReDoc</title><meta charset="utf-8"/></head>
<body>
  <redoc spec-url="/swagger.json"></redoc>
  <script src="https://unpkg.com/redoc@2/bundles/redoc.standalone.js"></script>
</body>
</html>`

func (s *Server) handleDocsUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(docsHTML))
}

func (s *Server) handleRedocUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(redocHTML))
}
