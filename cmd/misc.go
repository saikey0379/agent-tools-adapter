package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ---- config commands ----

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write config template to ~/.agent-tools/config.yaml",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigInit()
	},
}

func runConfigInit() error {
	home, _ := os.UserHomeDir()
	dest := filepath.Join(home, ".agent-tools", "config.yaml")
	if cfgFile != "" {
		dest = cfgFile
	}

	if _, err := os.Stat(dest); err == nil {
		fmt.Printf("Config already exists at %s\nEdit it directly to update settings.\n", dest)
		return nil
	}

	examplePaths := []string{
		filepath.Join(filepath.Dir(os.Args[0]), "config-example.yaml"),
		"/etc/agent-tools/config-example.yaml",
	}
	var src string
	for _, p := range examplePaths {
		if _, err := os.Stat(p); err == nil {
			src = p
			break
		}
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}

	if src != "" {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0600); err != nil {
			return err
		}
	} else {
		tmpl := `servers:
  default:
    openapi:
      url: https://agent-tools.example.com/openapi/api.json
      check_interval: 300
      headers:
        Authorization: "Bearer <your-token>"
        X-Role-Id: "<your-role-id>"
    mcp:
      url: https://agent-tools.example.com/mcp
      headers:
        Authorization: "Bearer <your-token>"
        X-Role-Id: "<your-role-id>"
llm:
  token: ""
log_file: /var/log/agent-tools.log
`
		if err := os.WriteFile(dest, []byte(tmpl), 0600); err != nil {
			return err
		}
	}

	fmt.Printf("Config template written to %s\nEdit it to fill in your url, token, and role_id.\n", dest)
	return nil
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current config (token masked)",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		path := filepath.Join(home, ".agent-tools", "config.yaml")
		if cfgFile != "" {
			path = cfgFile
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var raw map[string]any
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return err
		}
		maskRaw(raw)
		out, err := yaml.Marshal(raw)
		if err != nil {
			return err
		}
		fmt.Print(string(out))
		return nil
	},
}

// maskRaw walks the config map and masks Authorization header values in-place.
func maskRaw(m map[string]any) {
	for k, v := range m {
		switch val := v.(type) {
		case string:
			if strings.EqualFold(k, "api_key") {
				m[k] = maskToken(val)
			}
		case map[string]any:
			if strings.EqualFold(k, "headers") {
				for hk, hv := range val {
					if strings.EqualFold(hk, "authorization") {
						if s, ok := hv.(string); ok {
							val[hk] = maskToken(s)
						}
					}
				}
			} else {
				maskRaw(val)
			}
		}
	}
}

func maskToken(s string) string {
	if len(s) > 12 {
		return s[:8] + "..." + s[len(s)-4:]
	}
	return "***"
}

func init() {
	configCmd.AddCommand(configInitCmd, configShowCmd)
}
