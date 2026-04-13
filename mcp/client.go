package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"agent-tools/clog"
	"agent-tools/config"
	"agent-tools/tools"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// Client wraps the MCP SSE client with auth headers. No caching — tools are fetched live.
type Client struct {
	cfg *config.MCPConfig
}

func NewClient(cfg *config.MCPConfig) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) connect(ctx context.Context) (*mcpclient.Client, error) {
	sseURL := c.cfg.URL
	headers := make(map[string]string, len(c.cfg.Headers))
	for k, v := range c.cfg.Headers {
		if strings.EqualFold(k, "authorization") && !strings.HasPrefix(v, "Bearer ") {
			v = "Bearer " + v
		}
		headers[k] = v
	}
	mc, err := mcpclient.NewSSEMCPClient(sseURL, mcpclient.WithHeaders(headers))
	if err != nil {
		return nil, err
	}
	if err := mc.Start(ctx); err != nil {
		return nil, err
	}
	if _, err = mc.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		mc.Close()
		return nil, err
	}
	return mc, nil
}

func (c *Client) ListTools(ctx context.Context) ([]tools.ToolSchema, error) {
	mc, err := c.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcp connect: %w", err)
	}
	defer mc.Close()

	result, err := mc.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, err
	}
	return convertTools(result.Tools), nil
}

func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	clog.Info("mcp call tool=%s args=%s", name, clog.FormatArgs(args))
	doneConnect := clog.Timer("mcp connect")
	mc, err := c.connect(ctx)
	if err != nil {
		doneConnect("err=" + err.Error())
		return "", fmt.Errorf("mcp connect: %w", err)
	}
	doneConnect()
	defer mc.Close()

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	doneTool := clog.Timer("mcp tool=" + name)
	result, err := mc.CallTool(ctx, req)
	if err != nil {
		doneTool("err=" + err.Error())
		clog.Error("mcp tool=%s err=%v", name, err)
		return "", err
	}
	doneTool()

	var parts []string
	for _, content := range result.Content {
		if tc, ok := mcp.AsTextContent(content); ok {
			parts = append(parts, tc.Text)
		}
	}
	out := strings.Join(parts, "\n")
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err == nil {
		if _, ok := obj["$schema"]; ok {
			delete(obj, "$schema")
			if clean, err := json.Marshal(obj); err == nil {
				out = string(clean)
			}
		}
	}
	return out, nil
}

func convertTools(mcpTools []mcp.Tool) []tools.ToolSchema {
	out := make([]tools.ToolSchema, 0, len(mcpTools))
	for _, t := range mcpTools {
		ts := tools.ToolSchema{Name: tools.ToSnakeCase(t.Name), Description: t.Description}
		if t.InputSchema.Properties != nil {
			reqSet := map[string]bool{}
			for _, r := range t.InputSchema.Required {
				reqSet[r] = true
			}
			for name, prop := range t.InputSchema.Properties {
				p := tools.ToolParam{Name: name, Required: reqSet[name]}
				if m, ok := prop.(map[string]any); ok {
					if d, ok := m["description"].(string); ok {
						p.Description = d
					}
					if tp, ok := m["type"].(string); ok {
						p.Type = tp
					}
				}
				ts.Params = append(ts.Params, p)
			}
		}
		out = append(out, ts)
	}
	return out
}

// buildAnthropicTools converts tool schemas to JSON for use in agent.go
func BuildAnthropicSchema(t tools.ToolSchema) []byte {
	props := map[string]any{}
	required := []string{}
	for _, p := range t.Params {
		props[p.Name] = map[string]any{
			"type":        p.Type,
			"description": p.Description,
		}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	b, _ := json.Marshal(schema)
	return b
}
