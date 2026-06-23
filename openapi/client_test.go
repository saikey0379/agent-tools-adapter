package openapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"agent-tools/config"
	"agent-tools/tools"
)

func TestNewClientNormalizesLegacyCoraToolsURLs(t *testing.T) {
	c := NewClient(&config.OpenAPIConfig{
		URL:              "https://platform.example.com/openapi/api.json",
		CheckMD5:         "https://platform.example.com/openapi/api.md5",
		FilteredURL:      "https://platform.example.com/openapi/api.json",
		FilteredCheckMD5: "https://platform.example.com/openapi/api.md5",
	}, t.TempDir())

	if c.cfg.URL != "https://platform.example.com/openapi/avaiable_tools.json" {
		t.Fatalf("URL = %q", c.cfg.URL)
	}
	if c.cfg.CheckMD5 != "https://platform.example.com/openapi/avaiable_tools.md5" {
		t.Fatalf("CheckMD5 = %q", c.cfg.CheckMD5)
	}
	if c.cfg.FilteredURL != "https://platform.example.com/openapi/avaiable_tools.json" {
		t.Fatalf("FilteredURL = %q", c.cfg.FilteredURL)
	}
	if c.cfg.FilteredCheckMD5 != "https://platform.example.com/openapi/avaiable_tools.md5" {
		t.Fatalf("FilteredCheckMD5 = %q", c.cfg.FilteredCheckMD5)
	}
}

func TestNewClientDoesNotNormalizeOtherHosts(t *testing.T) {
	c := NewClient(&config.OpenAPIConfig{
		URL:      "https://agent-tools.example.com/openapi/api.json",
		CheckMD5: "https://agent-tools.example.com/openapi/api.md5",
	}, t.TempDir())

	if c.cfg.URL != "https://agent-tools.example.com/openapi/api.json" {
		t.Fatalf("URL = %q", c.cfg.URL)
	}
	if c.cfg.CheckMD5 != "https://agent-tools.example.com/openapi/api.md5" {
		t.Fatalf("CheckMD5 = %q", c.cfg.CheckMD5)
	}
}

func TestCallToolRefreshesStaleCacheBeforeReadingTool(t *testing.T) {
	var calledPath string
	spec := []byte(`{
		"openapi":"3.0.0",
		"paths":{
			"/new":{
				"get":{
					"operationId":"cachedTool",
					"summary":"Cached tool"
				}
			}
		}
	}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/md5":
			_, _ = w.Write([]byte(md5Hex(spec)))
		case "/openapi.json":
			_, _ = w.Write(spec)
		case "/new":
			calledPath = r.URL.Path
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/old":
			t.Fatalf("called stale cached path %s", r.URL.Path)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	if err := cache.Write([]tools.ToolSchema{{Name: "cached_tool", Method: http.MethodGet, Path: "/old"}}, "old-md5"); err != nil {
		t.Fatalf("write stale cache: %v", err)
	}

	c := NewClient(&config.OpenAPIConfig{URL: srv.URL + "/openapi.json", CheckMD5: srv.URL + "/md5"}, cacheDir)
	got, err := c.CallTool(context.Background(), "cached_tool", nil)
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if got != "{\n  \"ok\": true\n}" {
		t.Fatalf("CallTool() = %q", got)
	}
	if calledPath != "/new" {
		t.Fatalf("called path = %q", calledPath)
	}
}

func TestCallHTTPAcceptsExpandedBodyArgs(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient(&config.OpenAPIConfig{URL: srv.URL + "/openapi.json"}, t.TempDir())
	_, err := c.callHTTP(itemCreateTool(), map[string]any{
		"name":     "example item",
		"status":   "draft",
		"metadata": "{}",
	})
	if err != nil {
		t.Fatalf("callHTTP() error = %v", err)
	}
	if got["body"] != nil {
		t.Fatalf("request body was unexpectedly wrapped: %#v", got)
	}
	if got["name"] != "example item" {
		t.Fatalf("name = %#v", got["name"])
	}
}

func TestCallHTTPAcceptsWrappedBodyArg(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient(&config.OpenAPIConfig{URL: srv.URL + "/openapi.json"}, t.TempDir())
	_, err := c.callHTTP(itemCreateTool(), map[string]any{
		"body": map[string]any{
			"name":     "example item",
			"status":   "draft",
			"metadata": "{}",
		},
	})
	if err != nil {
		t.Fatalf("callHTTP() error = %v", err)
	}
	if got["body"] != nil {
		t.Fatalf("request body was unexpectedly wrapped: %#v", got)
	}
	if got["name"] != "example item" {
		t.Fatalf("name = %#v", got["name"])
	}
}

func TestParseSpecKeepsSummaryAndDescription(t *testing.T) {
	spec := []byte(`{
		"openapi":"3.0.0",
		"paths":{
			"/clusters":{
				"get":{
					"operationId":"listPlatformClusters",
					"summary":"列出系统集群（仅管理员，支持 type=option）",
					"description":"从工具元数据表覆盖后的集群查询长描述"
				}
			}
		}
	}`)

	got, err := parseSpec(spec)
	if err != nil {
		t.Fatalf("parseSpec() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(tools) = %d", len(got))
	}
	if got[0].Summary != "列出系统集群（仅管理员，支持 type=option）" {
		t.Fatalf("summary = %q", got[0].Summary)
	}
	if got[0].Description != "从工具元数据表覆盖后的集群查询长描述" {
		t.Fatalf("description = %q", got[0].Description)
	}
}

func TestParseSpecPreservesEnumConstraints(t *testing.T) {
	spec := []byte(`{
		"openapi":"3.0.0",
		"paths":{
			"/items":{
				"post":{
					"operationId":"createItem",
					"requestBody":{
						"content":{
							"application/json":{
								"schema":{
									"type":"object",
									"required":["name"],
									"properties":{
										"name":{"type":"string"},
										"status":{"type":"string","enum":["draft","active","archived"]},
										"tags":{"type":"array","items":{"type":"string"}},
										"metadata":{"type":"string"}
									}
								}
							}
						}
					}
				}
			}
		}
	}`)

	got, err := parseSpec(spec)
	if err != nil {
		t.Fatalf("parseSpec() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(tools) = %d", len(got))
	}
	params := map[string]tools.ToolParam{}
	for _, p := range got[0].Params {
		params[p.Name] = p
	}
	if params["metadata"].Type != "string" {
		t.Fatalf("metadata type = %q", params["metadata"].Type)
	}
	if params["tags"].Type != "array[string]" {
		t.Fatalf("tags type = %q", params["tags"].Type)
	}
	wantEnum := []any{"draft", "active", "archived"}
	if !reflect.DeepEqual(params["status"].Enum, wantEnum) {
		t.Fatalf("status enum = %#v", params["status"].Enum)
	}
}

func TestParseSpecCapturesResponseSchema(t *testing.T) {
	spec := []byte(`{
		"openapi":"3.0.0",
		"paths":{
			"/projects":{
				"get":{
					"operationId":"exportProjects",
					"responses":{
						"200":{
							"description":"OK",
							"content":{
								"application/json":{
									"schema":{
										"type":"object",
										"required":["data"],
										"properties":{
											"data":{"type":"array","items":{"$ref":"#/components/schemas/Project"}},
											"total":{"type":"integer","description":"Total count"}
										}
									}
								}
							}
						}
					}
				}
			}
		},
		"components":{
			"schemas":{
				"Project":{
					"required":["id"],
					"properties":{
						"id":{"type":"string","description":"Project ID"},
						"name":{"type":"string"}
					}
				}
			}
		}
	}`)

	got, err := parseSpec(spec)
	if err != nil {
		t.Fatalf("parseSpec() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(tools) = %d", len(got))
	}
	resp := got[0].Response
	if len(resp) != 2 {
		t.Fatalf("len(response) = %d, response = %#v", len(resp), resp)
	}
	if resp[0].Name != "data" || resp[0].Type != "array[Project]" || !resp[0].Required {
		t.Fatalf("data response field = %#v", resp[0])
	}
	if len(resp[0].Properties) != 2 || resp[0].Properties[0].Name != "id" || !resp[0].Properties[0].Required {
		t.Fatalf("data properties = %#v", resp[0].Properties)
	}
	if resp[1].Name != "total" || resp[1].Type != "integer" || resp[1].Description != "Total count" {
		t.Fatalf("total response field = %#v", resp[1])
	}
}

func itemCreateTool() *tools.ToolSchema {
	return &tools.ToolSchema{
		Name:   "create_item",
		Method: http.MethodPost,
		Path:   "/items",
		Params: []tools.ToolParam{
			{Name: "name", Type: "string", In: "body", Required: true},
			{Name: "status", Type: "string", In: "body", Enum: []any{"draft", "active", "archived"}},
			{Name: "metadata", Type: "string", In: "body"},
		},
	}
}
