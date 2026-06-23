package cmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-tools/config"
	"agent-tools/openapi"
	"agent-tools/tools"
)

func TestStripConfigArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantArgs   []string
		wantConfig string
	}{
		{
			name:       "long flag with value",
			args:       []string{"default/list_projects", "--config", "/tmp/plugin/config.yaml", "--page", "1"},
			wantArgs:   []string{"default/list_projects", "--page", "1"},
			wantConfig: "/tmp/plugin/config.yaml",
		},
		{
			name:       "short flag with value",
			args:       []string{"-c", "/tmp/plugin/config.yaml", "default/list_projects"},
			wantArgs:   []string{"default/list_projects"},
			wantConfig: "/tmp/plugin/config.yaml",
		},
		{
			name:       "equals form",
			args:       []string{"default/list_projects", "--config=/tmp/plugin/config.yaml"},
			wantArgs:   []string{"default/list_projects"},
			wantConfig: "/tmp/plugin/config.yaml",
		},
		{
			name:       "last config wins",
			args:       []string{"--config", "/tmp/old.yaml", "default/list_projects", "-c=/tmp/new.yaml"},
			wantArgs:   []string{"default/list_projects"},
			wantConfig: "/tmp/new.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotArgs, gotConfig := stripConfigArgs(tt.args)
			if gotConfig != tt.wantConfig {
				t.Fatalf("config = %q, want %q", gotConfig, tt.wantConfig)
			}
			if len(gotArgs) != len(tt.wantArgs) {
				t.Fatalf("args = %#v, want %#v", gotArgs, tt.wantArgs)
			}
			for i := range gotArgs {
				if gotArgs[i] != tt.wantArgs[i] {
					t.Fatalf("args = %#v, want %#v", gotArgs, tt.wantArgs)
				}
			}
		})
	}
}

func TestRunDescribeRefreshesWhenCachedListMissesTool(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()

	t.Setenv("HOME", t.TempDir())
	spec := []byte(`{
		"openapi":"3.0.0",
		"paths":{
			"/projects":{
				"get":{
					"operationId":"listProjects",
					"summary":"List projects",
					"responses":{
						"200":{
							"description":"OK",
							"content":{
								"application/json":{
									"schema":{
										"type":"object",
										"required":["data"],
										"properties":{
											"data":{"type":"array"}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(spec)
	}))
	defer srv.Close()

	cfg = &config.Config{Servers: map[string]config.ServerConfig{
		"default": {OpenAPI: &config.OpenAPIConfig{URL: srv.URL + "/openapi.json", CheckInterval: 3600}},
	}}
	cache := openapi.NewCache(config.CacheDir("default"))
	if err := cache.Write([]tools.ToolSchema{{Name: "old_tool", Method: http.MethodGet, Path: "/old"}}, "old-md5"); err != nil {
		t.Fatalf("write stale cache: %v", err)
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	err = runDescribe(context.Background(), "http", "", "list_projects", false)
	if closeErr := w.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	os.Stdout = oldStdout
	if err != nil {
		t.Fatalf("runDescribe() error = %v", err)
	}
	outBytes, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	out := string(outBytes)
	if !strings.Contains(out, "Response body (JSON):") {
		t.Fatalf("describe output missing JSON response heading:\n%s", out)
	}
}

func TestListDescriptionPrefersSummary(t *testing.T) {
	got := listDescription(tools.ToolSchema{
		Summary:     "Short summary",
		Description: "Long description",
	})
	if got != "Short summary" {
		t.Fatalf("listDescription() = %q", got)
	}
}

func TestPrintResponseFieldOmitsRequiredLabels(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	printResponseField(tools.ToolParam{
		Name:     "data",
		Type:     "array[Project]",
		Required: true,
		Properties: []tools.ToolParam{
			{Name: "id", Type: "string", Required: true},
		},
	}, 0)

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = oldStdout
	outBytes, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	out := string(outBytes)
	if strings.Contains(out, "required") {
		t.Fatalf("response output included required label:\n%s", out)
	}
}

func TestRootPersistentPreRunUsesConfigFlagBeforeLoading(t *testing.T) {
	oldArgs := os.Args
	oldCfgFile := cfgFile
	oldCfg := cfg
	defer func() {
		os.Args = oldArgs
		cfgFile = oldCfgFile
		cfg = oldCfg
	}()

	cfgFile = ""
	cfg = nil

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	data := []byte(`servers:
  default:
    openapi:
      url: http://127.0.0.1/openapi.json
`)
	if err := os.WriteFile(cfgPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	os.Args = []string{"agent-tools-cli", "default/list_projects", "--config", cfgPath}
	if err := rootCmd.PersistentPreRunE(rootCmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE() error = %v", err)
	}
	if cfgFile != cfgPath {
		t.Fatalf("cfgFile = %q, want %q", cfgFile, cfgPath)
	}
	if cfg == nil {
		t.Fatal("cfg was not loaded")
	}
}
