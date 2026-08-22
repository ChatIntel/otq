// Package semconv isolates otq's handling of the OTel GenAI semantic
// conventions in one place, since that convention has changed shape
// multiple times and instrumentation libraries don't agree on it.
//
// v1 pinned decision: otq targets the OpenLLMetry/Traceloop indexed-attribute
// dialect (gen_ai.prompt.N.content / .role, gen_ai.completion.N.*) as the
// primary supported input format. If gen_ai.input.messages /
// gen_ai.output.messages (newer semconv, often a JSON-encoded string) are
// present instead and the indexed form is absent, otq parses those as a
// fallback. If neither dialect is detected, Prompt/Completion stay nil
// (serialize to JSON null) — no silent misparse, no guessing.
package semconv

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"otq/internal/eval"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type GenAI struct {
	System        *string
	RequestModel  *string
	ResponseModel *string
	InputTokens   *float64
	OutputTokens  *float64
	ToolName      *string
	AgentName     *string
	Prompt        []Message // nil if neither dialect detected
	Completion    []Message
}

// flatFieldKeys maps the flat gen_ai.* attribute keys onto the GenAI struct
// fields that hold them (string-valued fields only; token counts are
// handled separately since they need numeric coercion).
var flatStringFields = map[string]func(*GenAI, string){
	"gen_ai.system":         func(g *GenAI, v string) { g.System = &v },
	"gen_ai.request.model":  func(g *GenAI, v string) { g.RequestModel = &v },
	"gen_ai.response.model": func(g *GenAI, v string) { g.ResponseModel = &v },
	"gen_ai.tool.name":      func(g *GenAI, v string) { g.ToolName = &v },
	"gen_ai.agent.name":     func(g *GenAI, v string) { g.AgentName = &v },
}

// LiftGenAI pops every gen_ai.* key it recognizes out of attrs and returns
// (GenAI, remaining attrs). The remaining map becomes the flattened span's
// catch-all `attributes` field.
func LiftGenAI(attrs map[string]eval.Value) (GenAI, map[string]eval.Value) {
	var g GenAI

	for key, setter := range flatStringFields {
		if v, ok := attrs[key]; ok && v.Kind == eval.KString {
			setter(&g, v.S)
			delete(attrs, key)
		}
	}
	if v, ok := attrs["gen_ai.usage.input_tokens"]; ok {
		if n, ok2 := numericValue(v); ok2 {
			g.InputTokens = &n
		}
		delete(attrs, "gen_ai.usage.input_tokens")
	}
	if v, ok := attrs["gen_ai.usage.output_tokens"]; ok {
		if n, ok2 := numericValue(v); ok2 {
			g.OutputTokens = &n
		}
		delete(attrs, "gen_ai.usage.output_tokens")
	}

	prompt, promptFound := liftIndexed(attrs, "gen_ai.prompt.")
	completion, completionFound := liftIndexed(attrs, "gen_ai.completion.")
	if promptFound || completionFound {
		g.Prompt = prompt
		g.Completion = completion
		return g, attrs
	}

	// Fallback dialect only tried when the indexed form was entirely absent.
	if msgs, ok := liftMessagesFallback(attrs, "gen_ai.input.messages"); ok {
		g.Prompt = msgs
	}
	if msgs, ok := liftMessagesFallback(attrs, "gen_ai.output.messages"); ok {
		g.Completion = msgs
	}

	return g, attrs
}

func numericValue(v eval.Value) (float64, bool) {
	switch v.Kind {
	case eval.KNumber:
		return v.N, true
	case eval.KString:
		if n, err := strconv.ParseFloat(v.S, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

// liftIndexed collects gen_ai.<prefix><N>.content / .role attributes into a
// []Message sorted by index N, popping every matched key from attrs.
// Returns found=false (and a nil slice) if no indexed keys were present at
// all, distinguishing "dialect absent" from "dialect present but empty".
func liftIndexed(attrs map[string]eval.Value, prefix string) (msgs []Message, found bool) {
	byIndex := map[int]*Message{}
	var matchedKeys []string

	for key := range attrs {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := key[len(prefix):] // "N.content" or "N.role"
		dot := strings.IndexByte(rest, '.')
		if dot == -1 {
			continue
		}
		idxStr, field := rest[:dot], rest[dot+1:]
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			continue
		}
		if field != "content" && field != "role" {
			continue
		}
		matchedKeys = append(matchedKeys, key)
		m, ok := byIndex[idx]
		if !ok {
			m = &Message{}
			byIndex[idx] = m
		}
		v := attrs[key]
		if v.Kind != eval.KString {
			continue
		}
		if field == "content" {
			m.Content = v.S
		} else {
			m.Role = v.S
		}
	}

	if len(matchedKeys) == 0 {
		return nil, false
	}
	for _, k := range matchedKeys {
		delete(attrs, k)
	}

	indices := make([]int, 0, len(byIndex))
	for idx := range byIndex {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	msgs = make([]Message, len(indices))
	for i, idx := range indices {
		msgs[i] = *byIndex[idx]
	}
	return msgs, true
}

// liftMessagesFallback parses gen_ai.input.messages / gen_ai.output.messages
// (newer semconv shape, typically a JSON-encoded string of [{role,content}]).
// On unmarshal failure, per otq's "no silent misparse" rule, the key is left
// untouched in attrs (visible in the catch-all) rather than popped or errored.
func liftMessagesFallback(attrs map[string]eval.Value, key string) ([]Message, bool) {
	v, ok := attrs[key]
	if !ok || v.Kind != eval.KString {
		return nil, false
	}
	var msgs []Message
	if err := json.Unmarshal([]byte(v.S), &msgs); err != nil {
		return nil, false
	}
	delete(attrs, key)
	return msgs, true
}
