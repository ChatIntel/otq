package semconv

import (
	"reflect"
	"testing"

	"otq/internal/eval"
)

func attrs(pairs ...any) map[string]eval.Value {
	m := map[string]eval.Value{}
	for i := 0; i < len(pairs); i += 2 {
		key := pairs[i].(string)
		switch v := pairs[i+1].(type) {
		case string:
			m[key] = eval.String(v)
		case float64:
			m[key] = eval.Number(v)
		}
	}
	return m
}

func TestLiftGenAI_FlatFields(t *testing.T) {
	a := attrs(
		"gen_ai.system", "openai",
		"gen_ai.request.model", "gpt-4",
		"gen_ai.usage.output_tokens", float64(42),
		"plain.attr", "keep-me",
	)
	g, remaining := LiftGenAI(a)
	if g.System == nil || *g.System != "openai" {
		t.Errorf("System = %v", g.System)
	}
	if g.RequestModel == nil || *g.RequestModel != "gpt-4" {
		t.Errorf("RequestModel = %v", g.RequestModel)
	}
	if g.OutputTokens == nil || *g.OutputTokens != 42 {
		t.Errorf("OutputTokens = %v", g.OutputTokens)
	}
	if _, ok := remaining["plain.attr"]; !ok {
		t.Errorf("non-gen_ai attribute was incorrectly popped")
	}
	if _, ok := remaining["gen_ai.system"]; ok {
		t.Errorf("gen_ai.system should have been lifted out of remaining")
	}
}

func TestLiftGenAI_IndexedDialect(t *testing.T) {
	a := attrs(
		"gen_ai.prompt.1.content", "second",
		"gen_ai.prompt.1.role", "user",
		"gen_ai.prompt.0.content", "first",
		"gen_ai.prompt.0.role", "system",
		"gen_ai.completion.0.content", "reply",
		"gen_ai.completion.0.role", "assistant",
	)
	g, remaining := LiftGenAI(a)
	want := []Message{{Role: "system", Content: "first"}, {Role: "user", Content: "second"}}
	if !reflect.DeepEqual(g.Prompt, want) {
		t.Errorf("Prompt = %#v, want %#v (sorted by index)", g.Prompt, want)
	}
	wantCompletion := []Message{{Role: "assistant", Content: "reply"}}
	if !reflect.DeepEqual(g.Completion, wantCompletion) {
		t.Errorf("Completion = %#v, want %#v", g.Completion, wantCompletion)
	}
	if len(remaining) != 0 {
		t.Errorf("remaining = %#v, want empty (all indexed keys popped)", remaining)
	}
}

func TestLiftGenAI_FallbackOnlyWhenIndexedAbsent(t *testing.T) {
	a := attrs(
		"gen_ai.input.messages", `[{"role":"user","content":"hi"}]`,
		"gen_ai.output.messages", `[{"role":"assistant","content":"hello"}]`,
	)
	g, remaining := LiftGenAI(a)
	want := []Message{{Role: "user", Content: "hi"}}
	if !reflect.DeepEqual(g.Prompt, want) {
		t.Errorf("Prompt = %#v, want %#v", g.Prompt, want)
	}
	if _, ok := remaining["gen_ai.input.messages"]; ok {
		t.Errorf("gen_ai.input.messages should be popped on successful parse")
	}
}

func TestLiftGenAI_IndexedWinsOverFallback(t *testing.T) {
	a := attrs(
		"gen_ai.prompt.0.content", "indexed wins",
		"gen_ai.prompt.0.role", "user",
		"gen_ai.input.messages", `[{"role":"user","content":"should be ignored"}]`,
	)
	g, remaining := LiftGenAI(a)
	if len(g.Prompt) != 1 || g.Prompt[0].Content != "indexed wins" {
		t.Errorf("Prompt = %#v, want indexed dialect to win", g.Prompt)
	}
	// fallback key untouched since indexed dialect already satisfied prompt/completion
	if _, ok := remaining["gen_ai.input.messages"]; !ok {
		t.Errorf("fallback key should remain untouched when indexed dialect wins")
	}
}

func TestLiftGenAI_MalformedFallbackLeavesRawKey(t *testing.T) {
	a := attrs("gen_ai.input.messages", "not valid json")
	g, remaining := LiftGenAI(a)
	if g.Prompt != nil {
		t.Errorf("Prompt = %#v, want nil on malformed fallback (no silent misparse)", g.Prompt)
	}
	v, ok := remaining["gen_ai.input.messages"]
	if !ok || v.S != "not valid json" {
		t.Errorf("malformed fallback key was popped or altered, want it left untouched in catch-all: %#v", remaining)
	}
}

func TestLiftGenAI_NeitherDialectDetected(t *testing.T) {
	a := attrs("name", "not-genai-related")
	g, _ := LiftGenAI(a)
	if g.Prompt != nil || g.Completion != nil {
		t.Errorf("Prompt/Completion = %#v/%#v, want both nil", g.Prompt, g.Completion)
	}
}

func TestLiftGenAI_TokenCountAsStringCoerces(t *testing.T) {
	a := map[string]eval.Value{
		"gen_ai.usage.input_tokens": eval.String("17"),
	}
	g, _ := LiftGenAI(a)
	if g.InputTokens == nil || *g.InputTokens != 17 {
		t.Errorf("InputTokens = %v, want 17 (coerced from string)", g.InputTokens)
	}
}
