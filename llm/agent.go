package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"agent-tools/clog"
	"agent-tools/config"
	"agent-tools/tools"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/sashabaranov/go-openai"
)

const systemPrompt = `You are a helpful assistant for the agent-tools platform CLI.
You have access to tools that manage Kubernetes clusters, platform resources, monitoring, and security.
When the user asks you to perform an action, call the appropriate tool with the correct parameters.
Always respond concisely. If a tool call returns data, summarize the key information.`

const recommendSystemPrompt = `You are a helpful assistant for the agent-tools platform CLI.
Based on the user's request and the available tools listed below, output the recommended agent-tools-cli command to run.
Output ONLY the command, no explanation. Format: agent-tools-cli <tool_name> [--param value ...]`

// Run executes an agentic loop: user input → LLM → tool calls → result.
// Both recommend and exec modes use function calling for consistency.
// In recommend mode, tool calls are converted to CLI commands instead of being executed.
// Uses OpenAI-compatible client when base_url is configured, otherwise Anthropic SDK.
func Run(ctx context.Context, cfg *config.Config, callers []tools.ServerCaller, serverName, toolName string, userInput string, recommend, raw bool) error {
	apiKey := cfg.LLMToken()
	if apiKey == "" {
		return fmt.Errorf("LLM token not set (config: llm.api_key or ANTHROPIC_API_KEY env var)")
	}

	// collect tools from all callers, tag each with its server name
	callerByServer := map[string]tools.Caller{}
	var toolList []tools.ToolSchema
	for _, sc := range callers {
		list, err := sc.Caller.ListTools(ctx)
		if err != nil {
			return fmt.Errorf("fetch tools from %s: %w", sc.Name, err)
		}
		for i := range list {
			list[i].ServerName = sc.Name
		}
		toolList = append(toolList, list...)
		callerByServer[sc.Name] = sc.Caller
	}

	// route tool call to the correct caller
	callTool := func(name string, args map[string]any) (string, error) {
		for _, t := range toolList {
			if t.Name == name {
				return callerByServer[t.ServerName].CallTool(ctx, name, args)
			}
		}
		return "", fmt.Errorf("tool %q not found", name)
	}

	if cfg.LLM.Type == "anthropic" {
		return runAnthropic(ctx, cfg, callTool, toolList, toolName, userInput, recommend, raw, apiKey)
	}
	return runOpenAI(ctx, cfg, callTool, toolList, toolName, userInput, recommend, raw, apiKey)
}

// toolCallToCLI converts a tool call to a agent-tools-cli command string.
func toolCallToCLI(name string, args map[string]any, toolList []tools.ToolSchema) string {
	serverName := "default"
	for _, t := range toolList {
		if t.Name == name {
			if t.ServerName != "" {
				serverName = t.ServerName
			}
			break
		}
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("agent-tools-cli -t mcp %s/%s", serverName, name))
	for k, v := range args {
		s := fmt.Sprintf("%v", v)
		if strings.Contains(s, " ") {
			sb.WriteString(fmt.Sprintf(" --%s %q", k, s))
		} else {
			sb.WriteString(fmt.Sprintf(" --%s %s", k, s))
		}
	}
	return sb.String()
}

// runOpenAI uses an OpenAI-compatible endpoint.
func runOpenAI(ctx context.Context, cfg *config.Config, callTool func(string, map[string]any) (string, error), toolList []tools.ToolSchema, toolName, userInput string, recommend, raw bool, apiKey string) error {
	ocfg := openai.DefaultConfig(apiKey)
	ocfg.BaseURL = strings.TrimRight(cfg.LLM.BaseURL, "/")
	client := openai.NewClientWithConfig(ocfg)

	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: userInput},
	}

	oaiTools := buildOpenAITools(toolList, toolName)

	for {
		req := openai.ChatCompletionRequest{
			Model:    cfg.LLM.ModelOrDefault("gpt-4o"),
			Messages: messages,
		}
		if len(oaiTools) > 0 {
			req.Tools = oaiTools
		}

		doneLLM := clog.Timer(fmt.Sprintf("llm openai round=%d model=%s", len(messages), cfg.LLM.ModelOrDefault("gpt-4o")))
		resp, err := client.CreateChatCompletion(ctx, req)
		if err != nil {
			doneLLM("err=" + err.Error())
			return fmt.Errorf("openai api: %w", err)
		}
		doneLLM()
		if len(resp.Choices) == 0 {
			return fmt.Errorf("openai api: empty response")
		}

		msg := resp.Choices[0].Message
		messages = append(messages, msg)

		if len(msg.ToolCalls) == 0 {
			if msg.Content != "" {
				fmt.Println(msg.Content)
			}
			return nil
		}

		for _, tc := range msg.ToolCalls {
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]any{}
			}
			if recommend {
				fmt.Println(toolCallToCLI(tc.Function.Name, args, toolList))
				return nil
			}
			fmt.Fprintf(os.Stderr, "→ calling %s\n", tc.Function.Name)
			doneTool := clog.Timer("llm tool=" + tc.Function.Name)
			output, callErr := callTool(tc.Function.Name, args)
			if callErr != nil {
				doneTool("err=" + callErr.Error())
			} else {
				doneTool()
			}
			content := output
			if callErr != nil {
				content = callErr.Error()
			}
			if raw {
				fmt.Println(content)
				return nil
			}
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				ToolCallID: tc.ID,
				Content:    content,
			})
		}
	}
}

// runAnthropic uses the native Anthropic SDK.
func runAnthropic(ctx context.Context, cfg *config.Config, callTool func(string, map[string]any) (string, error), toolList []tools.ToolSchema, toolName, userInput string, recommend, raw bool, apiKey string) error {
	anthropicTools := buildAnthropicTools(toolList, toolName)
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(userInput)),
	}

	for {
		params := anthropic.MessageNewParams{
			Model:     anthropic.Model(cfg.LLM.ModelOrDefault(string(anthropic.ModelClaudeSonnet4_6))),
			MaxTokens: 4096,
			System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
			Messages:  messages,
			Tools:     anthropicTools,
		}

		doneLLM := clog.Timer(fmt.Sprintf("llm anthropic round=%d model=%s", len(messages), cfg.LLM.ModelOrDefault(string(anthropic.ModelClaudeSonnet4_6))))
		resp, err := client.Messages.New(ctx, params)
		if err != nil {
			doneLLM("err=" + err.Error())
			return fmt.Errorf("claude api: %w", err)
		}
		doneLLM()

		messages = append(messages, resp.ToParam())

		var toolResults []anthropic.ContentBlockParamUnion
		hasToolUse := false

		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				fmt.Println(block.AsText().Text)
			case "tool_use":
				hasToolUse = true
				tu := block.AsToolUse()

				var args map[string]any
				if err := json.Unmarshal(tu.Input, &args); err != nil {
					args = map[string]any{}
				}

				if recommend {
					fmt.Println(toolCallToCLI(tu.Name, args, toolList))
					return nil
				}

				fmt.Fprintf(os.Stderr, "→ calling %s\n", tu.Name)
				doneTool := clog.Timer("llm tool=" + tu.Name)
				output, callErr := callTool(tu.Name, args)
				if callErr != nil {
					doneTool("err=" + callErr.Error())
				} else {
					doneTool()
				}
				if raw {
					if callErr != nil {
						fmt.Println(callErr.Error())
					} else {
						fmt.Println(output)
					}
					return nil
				}
				if callErr != nil {
					toolResults = append(toolResults, anthropic.NewToolResultBlock(tu.ID, callErr.Error(), true))
				} else {
					toolResults = append(toolResults, anthropic.NewToolResultBlock(tu.ID, output, false))
				}
			}
		}

		if !hasToolUse {
			return nil
		}

		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}
}

func buildOpenAITools(toolList []tools.ToolSchema, filterName string) []openai.Tool {
	var out []openai.Tool
	for _, t := range toolList {
		if filterName != "" && t.Name != filterName {
			continue
		}
		props := map[string]any{}
		required := []string{}
		for _, p := range t.Params {
			ptype := p.Type
			if ptype == "" {
				ptype = "string"
			}
			prop := map[string]any{
				"type":        ptype,
				"description": p.Description,
			}
			if ptype == "array" {
				prop["items"] = map[string]any{"type": "string"}
			}
			props[p.Name] = prop
			if p.Required {
				required = append(required, p.Name)
			}
		}
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		schemaBytes, _ := json.Marshal(schema)

		out = append(out, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  json.RawMessage(schemaBytes),
			},
		})
	}
	return out
}

func buildAnthropicTools(toolList []tools.ToolSchema, filterName string) []anthropic.ToolUnionParam {
	var out []anthropic.ToolUnionParam
	for _, t := range toolList {
		if filterName != "" && t.Name != filterName {
			continue
		}
		props := map[string]any{}
		required := []string{}
		for _, p := range t.Params {
			ptype := p.Type
			if ptype == "" {
				ptype = "string"
			}
			prop := map[string]any{
				"type":        ptype,
				"description": p.Description,
			}
			if ptype == "array" {
				prop["items"] = map[string]any{"type": "string"}
			}
			props[p.Name] = prop
			if p.Required {
				required = append(required, p.Name)
			}
		}
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		schemaBytes, _ := json.Marshal(schema)

		tool := anthropic.ToolUnionParamOfTool(
			anthropic.ToolInputSchemaParam{Properties: schemaBytes},
			t.Name,
		)
		tool.OfTool.Description = anthropic.String(t.Description)
		out = append(out, tool)
	}
	return out
}
