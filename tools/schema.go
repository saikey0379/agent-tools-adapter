package tools

import (
	"context"
	"strings"
	"unicode"
)

// ToolParam describes a single tool parameter
type ToolParam struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	In          string `json:"in,omitempty"` // path, query, body (openapi only)
}

// ToolSchema describes a single tool
type ToolSchema struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Method      string      `json:"method,omitempty"` // HTTP method (openapi only)
	Path        string      `json:"path,omitempty"`   // URL path template (openapi only)
	Params      []ToolParam `json:"params"`
	ServerName  string      `json:"server_name,omitempty"` // populated by LLM mode
}

// ServerCaller pairs a server name with its Caller.
type ServerCaller struct {
	Name   string
	Caller Caller
}

// Caller is implemented by both openapi and mcp clients
type Caller interface {
	ListTools(ctx context.Context) ([]ToolSchema, error)
	CallTool(ctx context.Context, name string, args map[string]any) (string, error)
}

// ToSnakeCase converts camelCase or kebab-case identifiers to snake_case.
func ToSnakeCase(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '-':
			b.WriteByte('_')
		case unicode.IsUpper(r) && i > 0 && runes[i-1] != '_' && runes[i-1] != '-' && !unicode.IsUpper(runes[i-1]):
			b.WriteByte('_')
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
