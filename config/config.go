package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type OpenAPIConfig struct {
	URL              string            `mapstructure:"url" yaml:"url"`
	FilteredURL      string            `mapstructure:"filtered_url" yaml:"filtered_url,omitempty"`           // role-filtered spec, used by default for --list
	CheckMD5         string            `mapstructure:"check_md5" yaml:"check_md5,omitempty"`                 // md5 URL for full spec
	FilteredCheckMD5 string            `mapstructure:"filtered_check_md5" yaml:"filtered_check_md5,omitempty"` // md5 URL for filtered spec
	CheckInterval    int               `mapstructure:"check_interval" yaml:"check_interval,omitempty"`       // seconds, fallback for both
	Headers          map[string]string `mapstructure:"headers" yaml:"headers,omitempty"`
}

type MCPConfig struct {
	URL     string            `mapstructure:"url" yaml:"url"`
	Headers map[string]string `mapstructure:"headers" yaml:"headers,omitempty"`
}

type ServerConfig struct {
	OpenAPI *OpenAPIConfig `mapstructure:"openapi" yaml:"openapi,omitempty"`
	MCP     *MCPConfig     `mapstructure:"mcp" yaml:"mcp,omitempty"`
}

type LLMConfig struct {
	Type    string `mapstructure:"type" yaml:"type,omitempty"`     // "openai" (default) or "anthropic"
	BaseURL string `mapstructure:"base_url" yaml:"base_url,omitempty"`
	APIKey  string `mapstructure:"api_key" yaml:"api_key,omitempty"`
	Model   string `mapstructure:"model" yaml:"model,omitempty"`   // e.g. "gpt-4.1", "claude-sonnet-4-6"
}

type Config struct {
	Servers map[string]ServerConfig `mapstructure:"servers" yaml:"servers"`
	LLM     LLMConfig               `mapstructure:"llm" yaml:"llm,omitempty"`
	LogFile string                  `mapstructure:"log_file" yaml:"log_file,omitempty"`
}

// Server returns the named server config, falling back to "default".
// Returns error if neither exists.
func (c *Config) Server(name string) (*ServerConfig, error) {
	if s, ok := c.Servers[name]; ok {
		return &s, nil
	}
	if name != "default" {
		return nil, fmt.Errorf("server %q not found in config", name)
	}
	return nil, fmt.Errorf("no servers configured")
}

// DefaultServer returns the "default" server config.
func (c *Config) DefaultServer() (*ServerConfig, error) {
	return c.Server("default")
}

// ModelOrDefault returns the configured model, or the given default if not set.
func (l LLMConfig) ModelOrDefault(def string) string {
	if l.Model != "" {
		return l.Model
	}
	return def
}

// LLMToken returns the LLM token, preferring ANTHROPIC_API_KEY env var.
func (c *Config) LLMToken() string {
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		return v
	}
	return c.LLM.APIKey
}

func configPath(cfgFile string) string {
	if cfgFile != "" {
		return cfgFile
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agent-tools", "config.yaml")
}

func Load(cfgFile string) (*Config, error) {
	viper.SetConfigFile(configPath(cfgFile))
	viper.SetEnvPrefix("agent-tools")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config not found — copy /etc/agent-tools/config-example.yaml to ~/.agent-tools/config.yaml")
		}
		return nil, fmt.Errorf("failed to read config %s: %w", configPath(cfgFile), err)
	}
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	if len(cfg.Servers) == 0 {
		return nil, fmt.Errorf("no servers configured — check your ~/.agent-tools/config.yaml")
	}
	def, err := cfg.DefaultServer()
	if err != nil {
		return nil, err
	}
	if def.OpenAPI == nil || def.OpenAPI.URL == "" {
		return nil, fmt.Errorf("default server openapi.url is required")
	}
	return &cfg, nil
}

func Init(cfgFile string, cfg *Config) error {
	path := configPath(cfgFile)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// CacheDir returns the base cache directory for a server.
func CacheDir(serverName string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agent-tools", "cache", serverName)
}
