package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"agent-tools/clog"
	"agent-tools/config"
	"agent-tools/tools"
)

// orderedProp holds a single schema property preserving JSON key order.
type orderedProp struct {
	Name        string
	Type        schemaType
	Description string
	Ref         string
	ItemsType   schemaType
	ItemsRef    string
}

// orderedProperties parses a JSON object's properties in declaration order.
type orderedProperties struct {
	props []orderedProp
}

func (o *orderedProperties) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if _, err := dec.Token(); err != nil {
		return err
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}
		var prop struct {
			Type        schemaType `json:"type"`
			Description string     `json:"description"`
			Ref         string     `json:"$ref"`
			Items       *struct {
				Type schemaType `json:"type"`
				Ref  string     `json:"$ref"`
			} `json:"items"`
		}
		if err := dec.Decode(&prop); err != nil {
			return err
		}
		p := orderedProp{
			Name:        key.(string),
			Type:        prop.Type,
			Description: prop.Description,
			Ref:         prop.Ref,
		}
		if prop.Items != nil {
			p.ItemsType = prop.Items.Type
			p.ItemsRef = prop.Items.Ref
		}
		o.props = append(o.props, p)
	}
	return nil
}

// schemaDef represents a components/schemas entry.
type schemaDef struct {
	Properties orderedProperties `json:"properties"`
	Required   []string          `json:"required"`
}

// refShortName extracts the last segment of a $ref path.
func refShortName(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

// resolveParams converts orderedProps to ToolParams, recursively expanding $ref.
// visited prevents infinite loops on circular refs.
func resolveParams(props []orderedProp, schemas map[string]schemaDef, in string, visited map[string]bool) []tools.ToolParam {
	var params []tools.ToolParam
	for _, p := range props {
		if strings.HasPrefix(p.Name, "$") {
			continue
		}
		param := tools.ToolParam{
			Name:        p.Name,
			Description: p.Description,
			In:          in,
		}
		t := p.Type
		switch {
		case t == "" && p.Ref != "":
			// object ref
			refName := refShortName(p.Ref)
			t = schemaType(refName)
			if !visited[refName] {
				if def, ok := schemas[refName]; ok {
					visited[refName] = true
					param.Properties = resolveParams(def.Properties.props, schemas, in, visited)
					delete(visited, refName)
				}
			}
		case t == "array":
			if p.ItemsRef != "" {
				refName := refShortName(p.ItemsRef)
				t = schemaType("array[" + refName + "]")
				if !visited[refName] {
					if def, ok := schemas[refName]; ok {
						visited[refName] = true
						param.Properties = resolveParams(def.Properties.props, schemas, in, visited)
						delete(visited, refName)
					}
				}
			} else if p.ItemsType != "" {
				t = schemaType("array[" + string(p.ItemsType) + "]")
			}
		}
		param.Type = string(t)
		params = append(params, param)
	}
	return params
}

// Client fetches OpenAPI spec, caches per-tool files, and calls tools via HTTP.
type Client struct {
	cfg   *config.OpenAPIConfig
	cache *Cache
}

func NewClient(cfg *config.OpenAPIConfig, cacheDir string) *Client {
	return &Client{cfg: cfg, cache: NewCache(cacheDir)}
}

func (c *Client) ListTools(ctx context.Context) ([]tools.ToolSchema, error) {
	if c.cache.IsValid(c.cfg.CheckMD5, c.cfg.CheckInterval, c.cfg.Headers) {
		return c.cache.ReadAllTools()
	}
	return c.fetchAndCache()
}

func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	clog.Info("http call tool=%s args=%s", name, clog.FormatArgs(args))
	done := clog.Timer("http tool=" + name)
	result, err := c.callToolInner(ctx, name, args)
	if err != nil {
		done("err=" + err.Error())
		clog.Error("http tool=%s err=%v", name, err)
	} else {
		done()
	}
	return result, err
}

func (c *Client) callToolInner(ctx context.Context, name string, args map[string]any) (string, error) {
	t, err := c.cache.ReadTool(name)
	if err != nil {
		// cache miss — refresh once
		toolList, ferr := c.fetchAndCache()
		if ferr != nil {
			return "", ferr
		}
		for i := range toolList {
			if toolList[i].Name == name {
				t = &toolList[i]
				break
			}
		}
		if t == nil {
			return "", fmt.Errorf("tool %q not found", name)
		}
	}
	return c.callHTTP(t, args)
}

func (c *Client) fetchAndCache() ([]tools.ToolSchema, error) {
	data, err := c.fetch(c.cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch openapi spec: %w", err)
	}
	toolList, err := parseSpec(data)
	if err != nil {
		return nil, fmt.Errorf("parse openapi spec: %w", err)
	}
	if werr := c.cache.Write(toolList, md5Hex(data)); werr != nil {
		fmt.Fprintf(os.Stderr, "warn: cache write failed: %v\n", werr)
	}
	return toolList, nil
}

func (c *Client) callHTTP(t *tools.ToolSchema, args map[string]any) (string, error) {
	baseURL := c.baseURL()
	path := t.Path
	query := url.Values{}
	body := map[string]any{}

	for _, p := range t.Params {
		val, ok := args[p.Name]
		if !ok {
			continue
		}
		s := fmt.Sprintf("%v", val)
		switch p.In {
		case "path":
			path = strings.ReplaceAll(path, "{"+p.Name+"}", url.PathEscape(s))
		case "query":
			query.Set(p.Name, s)
		case "body":
			// complex params (object/array with sub-fields) must be valid JSON
			if s, ok := val.(string); ok && len(s) > 0 {
				if s[0] == '{' || s[0] == '[' {
					var parsed any
					if err := json.Unmarshal([]byte(s), &parsed); err != nil {
						return "", fmt.Errorf("parameter %q: invalid JSON: %w", p.Name, err)
					}
					body[p.Name] = parsed
					continue
				} else if len(p.Properties) > 0 {
					return "", fmt.Errorf("parameter %q requires a JSON value (object or array)", p.Name)
				}
			}
			body[p.Name] = val
		}
	}

	reqURL := baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		b, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(b))
	}

	req, err := http.NewRequest(t.Method, reqURL, bodyReader)
	if err != nil {
		return "", err
	}
	for k, v := range c.cfg.Headers {
		if strings.EqualFold(k, "authorization") && !strings.HasPrefix(v, "Bearer ") && !strings.HasPrefix(v, "Basic ") {
			v = "Bearer " + v
		}
		req.Header.Set(k, v)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	doneHTTP := clog.Timer(fmt.Sprintf("http request method=%s url=%s", t.Method, reqURL))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		doneHTTP("err=" + err.Error())
		return "", err
	}
	doneHTTP(fmt.Sprintf("status=%d", resp.StatusCode))
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	// strip $schema field added by Huma framework, pretty print
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err == nil {
		delete(obj, "$schema")
		if clean, err := json.MarshalIndent(obj, "", "  "); err == nil {
			data = clean
		}
	}
	return string(data), nil
}

func (c *Client) baseURL() string {
	u, err := url.Parse(c.cfg.URL)
	if err != nil {
		return c.cfg.URL
	}
	return u.Scheme + "://" + u.Host
}

func (c *Client) fetch(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range c.cfg.Headers {
		if strings.EqualFold(k, "authorization") && !strings.HasPrefix(v, "Bearer ") && !strings.HasPrefix(v, "Basic ") {
			v = "Bearer " + v
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// parseSpec parses an OpenAPI 3.x JSON spec into tool schemas.
func parseSpec(data []byte) ([]tools.ToolSchema, error) {
	var spec struct {
		Paths      map[string]map[string]*opObject `json:"paths"`
		Components struct {
			Schemas map[string]schemaDef `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, err
	}

	var out []tools.ToolSchema
	for path, methods := range spec.Paths {
		for method, op := range methods {
			if op == nil || op.OperationID == "" {
				continue
			}
			t := tools.ToolSchema{
				Name:        tools.ToSnakeCase(op.OperationID),
				Description: op.Summary,
				Method:      strings.ToUpper(method),
				Path:        path,
			}
			if t.Description == "" {
				t.Description = op.Description
			}
			if t.Description == "" {
				t.Description = humanizeID(op.OperationID)
			}
			for _, p := range op.Parameters {
				t.Params = append(t.Params, tools.ToolParam{
					Name:        p.Name,
					Type:        string(p.Schema.Type),
					Description: p.Description,
					Required:    p.Required,
					In:          p.In,
				})
			}
			if op.RequestBody != nil {
				for _, ct := range op.RequestBody.Content {
					schema := ct.Schema
					// resolve top-level $ref if inline properties are absent
					if len(schema.Properties.props) == 0 && schema.Ref != "" {
						refName := refShortName(schema.Ref)
						if def, ok := spec.Components.Schemas[refName]; ok {
							schema.Properties = def.Properties
							if len(schema.Required) == 0 {
								schema.Required = def.Required
							}
						}
					}
					reqSet := map[string]bool{}
					for _, r := range schema.Required {
						reqSet[r] = true
					}
					params := resolveParams(schema.Properties.props, spec.Components.Schemas, "body", map[string]bool{})
					for i := range params {
						params[i].Required = reqSet[params[i].Name]
					}
					t.Params = append(t.Params, params...)
					break
				}
			}
			out = append(out, t)
		}
	}
	return out, nil
}

// humanizeID converts camelCase/snake_case operationId to a readable string.
func humanizeID(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		} else if r == '_' || r == '-' {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
type schemaType string

func (t *schemaType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*t = schemaType(s)
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	for _, v := range arr {
		if v != "null" {
			*t = schemaType(v)
			return nil
		}
	}
	if len(arr) > 0 {
		*t = schemaType(arr[0])
	}
	return nil
}

type opObject struct {
	OperationID string `json:"operationId"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Parameters  []struct {
		Name        string `json:"name"`
		In          string `json:"in"`
		Description string `json:"description"`
		Required    bool   `json:"required"`
		Schema      struct {
			Type schemaType `json:"type"`
		} `json:"schema"`
	} `json:"parameters"`
	RequestBody *struct {
		Content map[string]struct {
			Schema struct {
				Ref        string             `json:"$ref"`
				Properties orderedProperties  `json:"properties"`
				Required   []string           `json:"required"`
			} `json:"schema"`
		} `json:"content"`
	} `json:"requestBody"`
}
