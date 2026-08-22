package otlp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"otq/internal/otlperr"
)

// RawSpan is one OTLP span together with the resource/scope it belongs to,
// as parsed straight from an input file — before internal/flatten projects
// it into otq's query-facing Span shape.
type RawSpan struct {
	Span     Span
	Resource Resource
	Scope    Scope
}

func hasGlobMeta(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

// resolveFiles expands each input path into a concrete, deduplicated list
// of .json/.jsonl files: directories are globbed non-recursively, glob
// patterns are expanded, and literal file paths are passed through as-is.
func resolveFiles(paths []string) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	add := func(f string) {
		if !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}

	for _, p := range paths {
		info, err := os.Stat(p)
		switch {
		case err == nil && info.IsDir():
			for _, pattern := range []string{"*.json", "*.jsonl"} {
				matches, err := filepath.Glob(filepath.Join(p, pattern))
				if err != nil {
					return nil, &otlperr.InputError{Source: p, Msg: err.Error()}
				}
				sort.Strings(matches)
				for _, m := range matches {
					add(m)
				}
			}
		case err == nil:
			add(p)
		case hasGlobMeta(p):
			matches, gerr := filepath.Glob(p)
			if gerr != nil {
				return nil, &otlperr.InputError{Source: p, Msg: gerr.Error()}
			}
			if len(matches) == 0 {
				return nil, &otlperr.InputError{Source: p, Msg: "no files matched"}
			}
			sort.Strings(matches)
			for _, m := range matches {
				add(m)
			}
		default:
			return nil, &otlperr.InputError{Source: p, Msg: err.Error()}
		}
	}
	return files, nil
}

// LoadInputs resolves paths (files, directories, or glob patterns) into
// .json/.jsonl input files, parses each, and merges every span across every
// file/batch into one flat []RawSpan set.
func LoadInputs(paths []string) ([]RawSpan, error) {
	files, err := resolveFiles(paths)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, &otlperr.InputError{Source: strings.Join(paths, ", "), Msg: "no input files found"}
	}

	var spans []RawSpan
	for _, f := range files {
		switch strings.ToLower(filepath.Ext(f)) {
		case ".json":
			s, err := loadJSONFile(f)
			if err != nil {
				return nil, err
			}
			spans = append(spans, s...)
		case ".jsonl":
			s, err := loadJSONLFile(f)
			if err != nil {
				return nil, err
			}
			spans = append(spans, s...)
		default:
			return nil, &otlperr.InputError{Source: f, Msg: "unsupported file extension (expected .json or .jsonl)"}
		}
	}
	return spans, nil
}

func spansFromRequest(req ExportTraceServiceRequest) []RawSpan {
	var out []RawSpan
	for _, rs := range req.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, sp := range ss.Spans {
				out = append(out, RawSpan{Span: sp, Resource: rs.Resource, Scope: ss.Scope})
			}
		}
	}
	return out
}

func loadJSONFile(path string) ([]RawSpan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &otlperr.InputError{Source: path, Msg: err.Error()}
	}
	var req ExportTraceServiceRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, &otlperr.InputError{Source: path, Msg: fmt.Sprintf("malformed JSON: %v", err)}
	}
	return spansFromRequest(req), nil
}

// loadJSONLFile parses one OTLP JSONL file: one complete
// ExportTraceServiceRequest JSON object per line (one full export batch,
// potentially containing multiple resourceSpans) — NOT one line per span.
func loadJSONLFile(path string) ([]RawSpan, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, &otlperr.InputError{Source: path, Msg: err.Error()}
	}
	defer f.Close()

	var spans []RawSpan
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024) // tolerate long lines with embedded prompt/completion text
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req ExportTraceServiceRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			return nil, &otlperr.InputError{Source: fmt.Sprintf("%s:%d", path, lineNo), Msg: fmt.Sprintf("malformed JSON: %v", err)}
		}
		spans = append(spans, spansFromRequest(req)...)
	}
	if err := scanner.Err(); err != nil {
		return nil, &otlperr.InputError{Source: path, Msg: err.Error()}
	}
	return spans, nil
}
