package openapi

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"agent-tools/tools"
)

const md5File = "openapi.md5"
const tsFile = "openapi.ts"
const toolsSubDir = "tools"

// defaultCheckInterval is used when neither check_md5 nor check_interval is configured.
// Without a staleness signal the cache would be held forever, so fall back to a
// conservative TTL (10 minutes) to bound drift.
const defaultCheckInterval = 600

// Cache manages per-tool JSON files under dir with global MD5 consistency.
type Cache struct {
	dir string
}

func NewCache(dir string) *Cache {
	return &Cache{dir: dir}
}

func (c *Cache) toolFile(name string) string {
	return filepath.Join(c.dir, toolsSubDir, name+".json")
}

// IsValid checks cache freshness.
// If checkMD5URL is set, fetches remote MD5 and compares (takes priority).
// If checkInterval > 0, checks if cache is older than interval seconds.
// If neither is configured, falls back to defaultCheckInterval — a non-empty
// cache is never treated as valid indefinitely.
func (c *Cache) IsValid(checkMD5URL string, checkInterval int, headers map[string]string) bool {
	if checkMD5URL != "" && checkInterval > 0 {
		// check_md5 takes priority per spec
		return c.checkByMD5(checkMD5URL, headers)
	}
	if checkMD5URL != "" {
		return c.checkByMD5(checkMD5URL, headers)
	}
	if checkInterval > 0 {
		return c.checkByInterval(checkInterval)
	}
	if !c.hasTools() {
		return false
	}
	return c.checkByInterval(defaultCheckInterval)
}

func (c *Cache) checkByMD5(url string, headers map[string]string) bool {
	remote, err := fetchText(url, headers)
	if err != nil {
		return false
	}
	local, err := os.ReadFile(filepath.Join(c.dir, md5File))
	if err != nil {
		return false
	}
	return string(local) == remote
}

func (c *Cache) checkByInterval(intervalSec int) bool {
	data, err := os.ReadFile(filepath.Join(c.dir, tsFile))
	if err != nil {
		return false
	}
	var ts time.Time
	if err := json.Unmarshal(data, &ts); err != nil {
		return false
	}
	return time.Since(ts) < time.Duration(intervalSec)*time.Second
}

func (c *Cache) hasTools() bool {
	entries, err := os.ReadDir(filepath.Join(c.dir, toolsSubDir))
	return err == nil && len(entries) > 0
}

// ReadTool reads a single tool schema from cache.
func (c *Cache) ReadTool(name string) (*tools.ToolSchema, error) {
	data, err := os.ReadFile(c.toolFile(name))
	if err != nil {
		return nil, fmt.Errorf("tool %q not in cache", name)
	}
	var t tools.ToolSchema
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// ReadAllTools reads all cached tool schemas.
func (c *Cache) ReadAllTools() ([]tools.ToolSchema, error) {
	dir := filepath.Join(c.dir, toolsSubDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cache empty — run with --refresh or check config")
	}
	var out []tools.ToolSchema
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var t tools.ToolSchema
		if err := json.Unmarshal(data, &t); err != nil {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// Write writes all tool schemas to per-tool files and updates MD5/ts.
// Tools absent from toolList are pruned so the cache matches the current spec.
func (c *Cache) Write(toolList []tools.ToolSchema, specMD5 string) error {
	dir := filepath.Join(c.dir, toolsSubDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	keep := make(map[string]struct{}, len(toolList))
	for _, t := range toolList {
		data, err := json.Marshal(t)
		if err != nil {
			return fmt.Errorf("marshal tool %q: %w", t.Name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, t.Name+".json"), data, 0600); err != nil {
			return err
		}
		keep[t.Name+".json"] = struct{}{}
	}
	// Prune stale tools that no longer exist in the current spec.
	// Run after all writes succeed so a partial failure leaves the old cache intact.
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			if _, ok := keep[e.Name()]; !ok {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	if specMD5 != "" {
		_ = os.WriteFile(filepath.Join(c.dir, md5File), []byte(specMD5), 0600)
	}
	ts, _ := json.Marshal(time.Now())
	_ = os.WriteFile(filepath.Join(c.dir, tsFile), ts, 0600)
	return nil
}

func fetchText(url string, headers map[string]string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func md5Hex(data []byte) string {
	return fmt.Sprintf("%x", md5.Sum(data))
}
