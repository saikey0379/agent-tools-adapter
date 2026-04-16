package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"agent-tools/clog"
	"agent-tools/config"
	"agent-tools/llm"
	mcppkg "agent-tools/mcp"
	openapiPkg "agent-tools/openapi"
	"agent-tools/tools"

	"github.com/spf13/cobra"
)

var (
	cfgFile string
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "agent-tools-cli [-t http|mcp|llm] [-l | <tool_name> [-d] [flags...]]",
	Short: "agent-tools CLI — universal tool adapter",
	Long: `agent-tools-cli is a universal adapter for calling platform tools via http, mcp, or llm.

  agent-tools-cli -l                       list all tools (http, default)
  agent-tools-cli -t mcp -l               list all tools via mcp
  agent-tools-cli <tool> -d               show tool parameters
  agent-tools-cli <tool> [--param value ...]  call tool directly
  agent-tools-cli -t llm "..."            LLM recommend mode (outputs CLI command, no execution)
  agent-tools-cli -t llm "..." -e         LLM execute mode (calls tools automatically)
  agent-tools-cli -t llm "..." -e -r      LLM execute mode, output raw JSON result

Flags:
  -t, --type      caller type: http (default), mcp, llm
  -l, --list      list available tools
  -d, --describe  show tool parameters
  -e, --exec      execute tool via LLM (default: recommend only)
  -r, --raw       raw tool response without LLM summary (llm -e only)

Use -l and -d to discover available tools and their parameters.`,
	DisableFlagParsing: true,
	Args:               cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		rawArgs := os.Args[1:]

		// check if first real arg is a known subcommand — let cobra handle it
		for _, arg := range rawArgs {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			switch arg {
			case "context", "config", "completion", "help":
				return cmd.Help()
			}
			break
		}

		if err := loadConfig(); err != nil {
			return err
		}

		listAll, listFull, listFilter, describe, callerType, serverName, toolName, nlInput, recommend, raw, params := parseArgs(rawArgs)
		ctx := context.Background()

		if listAll {
			sn := serverName
			if sn == "" && toolName != "" {
				sn = toolName
			}
			return runList(ctx, callerType, sn, listFull, listFilter)
		}
		if toolName == "" {
			return cmd.Help()
		}
		if describe {
			return runDescribe(ctx, callerType, serverName, toolName)
		}

		if callerType == "llm" {
			input := nlInput
			if input == "" {
				input = toolName
				toolName = ""
			}
			callers, err := buildLLMCallers(ctx, serverName)
			if err != nil {
				return err
			}
			return llm.Run(ctx, cfg, callers, serverName, toolName, input, recommend, raw)
		}

		caller, err := newCaller(ctx, callerType, serverName)
		if err != nil {
			return err
		}
		result, err := caller.CallTool(ctx, toolName, params)
		if err != nil {
			return err
		}
		fmt.Println(result)
		return nil
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "init" && cmd.Parent() != nil && cmd.Parent().Name() == "config" {
			return nil
		}
		return loadConfig()
	},
}

// buildLLMCallers builds the list of ServerCallers for LLM mode.
// If serverName is specified, only that server is used.
// Otherwise, "default" is tried first, then all other servers with MCP config.
func buildLLMCallers(ctx context.Context, serverName string) ([]tools.ServerCaller, error) {
	if serverName != "" {
		caller, err := newCaller(ctx, "llm", serverName)
		if err != nil {
			return nil, err
		}
		return []tools.ServerCaller{{Name: serverName, Caller: caller}}, nil
	}

	var callers []tools.ServerCaller
	// default first
	if _, ok := cfg.Servers["default"]; ok {
		if caller, err := newCaller(ctx, "llm", "default"); err == nil {
			callers = append(callers, tools.ServerCaller{Name: "default", Caller: caller})
		}
	}
	// then others
	for name := range cfg.Servers {
		if name == "default" {
			continue
		}
		srv, _ := cfg.Server(name)
		if srv.MCP == nil {
			continue
		}
		if caller, err := newCaller(ctx, "llm", name); err == nil {
			callers = append(callers, tools.ServerCaller{Name: name, Caller: caller})
		}
	}
	if len(callers) == 0 {
		return nil, fmt.Errorf("no mcp servers configured")
	}
	return callers, nil
}

func loadConfig() error {
	if cfg != nil {
		return nil
	}
	var err error
	cfg, err = config.Load(cfgFile)
	if err != nil {
		return err
	}
	clog.Init(cfg.LogFile)
	return nil
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default: ~/.agent-tools/config.yaml)")
	rootCmd.AddCommand(configCmd)
}

// newCaller builds the appropriate Caller for the given callerType and server.
func newCaller(ctx context.Context, callerType, serverName string) (tools.Caller, error) {
	if serverName == "" {
		serverName = "default"
	}
	srv, err := cfg.Server(serverName)
	if err != nil {
		return nil, err
	}
	switch callerType {
	case "mcp", "llm":
		if srv.MCP == nil {
			return nil, fmt.Errorf("server %q has no mcp config", serverName)
		}
		return mcppkg.NewClient(srv.MCP), nil
	default: // http
		if srv.OpenAPI == nil {
			return nil, fmt.Errorf("server %q has no openapi config", serverName)
		}
		return openapiPkg.NewClient(srv.OpenAPI, config.CacheDir(serverName)), nil
	}
}

func runList(ctx context.Context, callerType, serverName string, listFull bool, filter string) error {
	printTools := func(name string, toolList []tools.ToolSchema) {
		for _, t := range toolList {
			if filter != "" {
				haystack := strings.ToLower(t.Name + " " + t.Description)
				if !strings.Contains(haystack, strings.ToLower(filter)) {
					continue
				}
			}
			fmt.Printf("%-40s %s\n", name+"/"+t.Name, firstLine(t.Description))
		}
	}
	if serverName != "" {
		caller, err := newCallerForList(ctx, callerType, serverName, listFull)
		if err != nil {
			return err
		}
		toolList, err := caller.ListTools(ctx)
		if err != nil {
			return err
		}
		printTools(serverName, toolList)
		return nil
	}
	for name := range cfg.Servers {
		srv, _ := cfg.Server(name)
		switch callerType {
		case "mcp", "llm":
			if srv.MCP == nil {
				continue
			}
		default:
			if srv.OpenAPI == nil {
				continue
			}
		}
		caller, err := newCallerForList(ctx, callerType, name, listFull)
		if err != nil {
			fmt.Printf("# server %s: %v\n", name, err)
			continue
		}
		toolList, err := caller.ListTools(ctx)
		if err != nil {
			fmt.Printf("# server %s: %v\n", name, err)
			continue
		}
		printTools(name, toolList)
	}
	return nil
}

// newCallerForList is like newCaller but uses filtered_url for HTTP --list (unless listFull).
func newCallerForList(ctx context.Context, callerType, serverName string, listFull bool) (tools.Caller, error) {
	if serverName == "" {
		serverName = "default"
	}
	srv, err := cfg.Server(serverName)
	if err != nil {
		return nil, err
	}
	switch callerType {
	case "mcp", "llm":
		if srv.MCP == nil {
			return nil, fmt.Errorf("server %q has no mcp config", serverName)
		}
		return mcppkg.NewClient(srv.MCP), nil
	default: // http
		if srv.OpenAPI == nil {
			return nil, fmt.Errorf("server %q has no openapi config", serverName)
		}
		ocfg := srv.OpenAPI
		if !listFull && ocfg.FilteredURL != "" {
			filtered := *ocfg
			filtered.URL = ocfg.FilteredURL
			filtered.CheckMD5 = ocfg.FilteredCheckMD5
			return openapiPkg.NewClient(&filtered, config.CacheDir(serverName+"-filtered")), nil
		}
		return openapiPkg.NewClient(ocfg, config.CacheDir(serverName)), nil
	}
}

func runDescribe(ctx context.Context, callerType, serverName, toolName string) error {
	caller, err := resolveCallerForTool(ctx, callerType, serverName, toolName)
	if err != nil {
		return err
	}
	toolList, err := caller.ListTools(ctx)
	if err != nil {
		return err
	}
	for _, t := range toolList {
		if t.Name == toolName {
			fmt.Printf("Tool:        %s\n", t.Name)
			fmt.Printf("Description: %s\n\n", t.Description)
			fmt.Printf("Parameters:\n")
			for _, p := range t.Params {
				req := ""
				if p.Required {
					req = " (required)"
				}
				fmt.Printf("  --%-25s %s [%s]%s\n", p.Name, p.Description, p.Type, req)
			}
			return nil
		}
	}
	return fmt.Errorf("tool %q not found", toolName)
}

// resolveCallerForTool tries default server first, then others.
// If serverName is specified, only that server is tried.
func resolveCallerForTool(ctx context.Context, callerType, serverName, toolName string) (tools.Caller, error) {
	if serverName != "" {
		return newCaller(ctx, callerType, serverName)
	}
	if _, ok := cfg.Servers["default"]; ok {
		if caller, err := newCaller(ctx, callerType, "default"); err == nil {
			if toolList, err := caller.ListTools(ctx); err == nil {
				for _, t := range toolList {
					if t.Name == toolName {
						return caller, nil
					}
				}
			}
		}
	}
	for name := range cfg.Servers {
		if name == "default" {
			continue
		}
		caller, err := newCaller(ctx, callerType, name)
		if err != nil {
			continue
		}
		toolList, err := caller.ListTools(ctx)
		if err != nil {
			continue
		}
		for _, t := range toolList {
			if t.Name == toolName {
				return caller, nil
			}
		}
	}
	return nil, fmt.Errorf("tool %q not found in any server", toolName)
}

func firstLine(s string) string {
	lines := strings.Split(s, "\n")
	first := ""
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "description:") {
			val := strings.TrimSpace(line[len("description:"):])
			if val != "" {
				return val
			}
			// description: was empty — take next non-empty line
			for _, next := range lines[i+1:] {
				next = strings.TrimSpace(next)
				if next != "" {
					return next
				}
			}
			return first
		}
		if first == "" {
			first = line
		}
	}
	return first
}

// parseArgs manually parses raw CLI args.
func parseArgs(args []string) (listAll, listFull bool, listFilter string, describe bool, callerType, serverName, toolName, nlInput string, recommend, raw bool, params map[string]any) {
	params = map[string]any{}
	callerType = "http"
	recommend = true

	// strip --config <val> first so it doesn't confuse tool arg parsing
	var cleaned []string
	for i := 0; i < len(args); i++ {
		if (args[i] == "--config" || args[i] == "-config" || args[i] == "-c") && i+1 < len(args) {
			i++ // skip value too
			continue
		}
		cleaned = append(cleaned, args[i])
	}

	i := 0
	for i < len(cleaned) {
		arg := cleaned[i]
		switch {
		case arg == "--list" || arg == "-l":
			listAll = true
			if i+1 < len(cleaned) && !strings.HasPrefix(cleaned[i+1], "-") {
				next := cleaned[i+1]
				if next == "all" {
					listFull = true
				} else {
					listFilter = next
				}
				i++
			}
		case arg == "--describe" || arg == "-d":
			describe = true
		case arg == "--exec" || arg == "--execute" || arg == "-e":
			recommend = false
		case arg == "--recommend":
			recommend = true
		case arg == "--raw" || arg == "-r":
			raw = true
		case (arg == "--type" || arg == "-t") && i+1 < len(cleaned):
			i++
			callerType = cleaned[i]
		case strings.HasPrefix(arg, "--type="):
			callerType = strings.TrimPrefix(arg, "--type=")
		case strings.HasPrefix(arg, "--") && i+1 < len(cleaned) && !strings.HasPrefix(cleaned[i+1], "--"):
			key := strings.TrimPrefix(arg, "--")
			var vals []string
			for i+1 < len(cleaned) && !strings.HasPrefix(cleaned[i+1], "--") {
				i++
				vals = append(vals, cleaned[i])
			}
			params[key] = strings.Join(vals, " ")
		case strings.HasPrefix(arg, "--"):
			key := strings.TrimPrefix(arg, "--")
			params[key] = true
		case toolName == "":
			if idx := strings.Index(arg, "/"); idx > 0 && isIdentifier(arg[:idx]) && isIdentifier(arg[idx+1:]) {
				serverName = arg[:idx]
				toolName = arg[idx+1:]
			} else {
				toolName = arg
			}
		default:
			if nlInput == "" {
				nlInput = arg
			} else {
				nlInput += " " + arg
			}
		}
		i++
	}
	return
}

// isIdentifier returns true if s is a valid tool/server identifier: only ASCII letters, digits, underscores, hyphens.
func isIdentifier(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
