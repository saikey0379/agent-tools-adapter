package cmd

import (
	"os"
	"path/filepath"
	"testing"
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
