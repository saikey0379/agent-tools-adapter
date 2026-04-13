package clog

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"
)

var logger *log.Logger

// Init sets up the file logger. Safe to call multiple times; only first call takes effect.
func Init(logFile string) {
	if logger != nil {
		return
	}
	if logFile == "" {
		logFile = "/var/log/agent-tools.log"
	}
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	logger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
}

func logf(level, format string, args ...any) {
	if logger == nil {
		return
	}
	logger.Printf("["+level+"] "+format, args...)
}

// Timer returns a function that logs elapsed time when called.
func Timer(label string) func(extra ...string) {
	start := time.Now()
	return func(extra ...string) {
		msg := fmt.Sprintf("%s elapsed=%.3fms", label, float64(time.Since(start).Microseconds())/1000)
		if len(extra) > 0 {
			msg += " " + extra[0]
		}
		logf("INFO", "%s", msg)
	}
}

func Info(format string, args ...any)  { logf("INFO", format, args...) }
func Error(format string, args ...any) { logf("ERROR", format, args...) }

// FormatArgs formats a map[string]any as key=value pairs, quoting values that contain spaces.
func FormatArgs(args map[string]any) string {
	if len(args) == 0 {
		return "[]"
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteByte('[')
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		v := fmt.Sprintf("%v", args[k])
		sb.WriteString(k)
		sb.WriteByte('=')
		if strings.ContainsAny(v, " \t") {
			sb.WriteString(fmt.Sprintf("%q", v))
		} else {
			sb.WriteString(v)
		}
	}
	sb.WriteByte(']')
	return sb.String()
}
