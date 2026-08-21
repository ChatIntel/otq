package eval

import "otq/internal/parser"

// spanIndex resolves parent/child relationships across the full,
// unfiltered span set, built once per query from the original input
// (see pipelineCtx) — nav stages traverse the whole export's tree, not
// whatever subset the stream has been filtered down to so far.
type spanIndex struct {
	bySpanID map[string]Value
	byParent map[string][]Value // parent_span_id -> children, in input order
}

func buildSpanIndex(spans []Value) *spanIndex {
	idx := &spanIndex{bySpanID: map[string]Value{}, byParent: map[string][]Value{}}
	for _, s := range spans {
		id := s.Get([]string{"span_id"})
		if id.Kind == KString {
			idx.bySpanID[id.S] = s
		}
		parent := s.Get([]string{"parent_span_id"})
		if parent.Kind == KString {
			idx.byParent[parent.S] = append(idx.byParent[parent.S], s)
		}
	}
	return idx
}

func (idx *spanIndex) children(s Value) []Value {
	id := s.Get([]string{"span_id"})
	if id.Kind != KString {
		return nil
	}
	return idx.byParent[id.S]
}

// descendants walks the child tree breadth-first-ish via recursion,
// guarding against cycles/duplicate edges in malformed input with a
// seen-span_id set (valid OTel data is acyclic, but otq never trusts that).
func (idx *spanIndex) descendants(s Value) []Value {
	var out []Value
	seen := map[string]bool{}
	var walk func(Value)
	walk = func(cur Value) {
		for _, c := range idx.children(cur) {
			cid := c.Get([]string{"span_id"}).S
			if seen[cid] {
				continue
			}
			seen[cid] = true
			out = append(out, c)
			walk(c)
		}
	}
	walk(s)
	return out
}

// parentOf looks up s's parent span. Returns ok=false both when s has no
// parent_span_id and when it references a span absent from this export
// (a dangling reference in a partial export) — either way, there's no
// parent span object to return.
func (idx *spanIndex) parentOf(s Value) (Value, bool) {
	pid := s.Get([]string{"parent_span_id"})
	if pid.Kind != KString {
		return Value{}, false
	}
	p, ok := idx.bySpanID[pid.S]
	return p, ok
}

func (idx *spanIndex) ancestors(s Value) []Value {
	var out []Value
	seen := map[string]bool{}
	cur := s
	for {
		p, ok := idx.parentOf(cur)
		if !ok {
			return out
		}
		pid := p.Get([]string{"span_id"}).S
		if seen[pid] {
			return out // cycle guard
		}
		seen[pid] = true
		out = append(out, p)
		cur = p
	}
}

// root walks up to the topmost ancestor. If s has no parent (or its
// parent chain hits a dangling reference), s itself is the root.
func (idx *spanIndex) root(s Value) Value {
	cur := s
	seen := map[string]bool{}
	for {
		p, ok := idx.parentOf(cur)
		if !ok {
			return cur
		}
		pid := p.Get([]string{"span_id"}).S
		if seen[pid] {
			return cur // cycle guard
		}
		seen[pid] = true
		cur = p
	}
}

// stageNav applies a nav operation to every value in the stream and
// de-duplicates the combined result by span_id (preserving first-occurrence
// order), since a set of related spans is a more useful result than a
// stream with the same span repeated for multiple inputs. A value with no
// span_id (e.g. the stream is bucket- or project()-shaped, not span-shaped)
// simply contributes nothing — nav is only meaningful over span-shaped
// values, and otq never crashes on a shape mismatch.
func stageNav(st parser.NavStage, values []Value, idx *spanIndex) []Value {
	var out []Value
	seen := map[string]bool{}
	add := func(v Value) {
		id := v.Get([]string{"span_id"})
		if id.Kind != KString || seen[id.S] {
			return
		}
		seen[id.S] = true
		out = append(out, v)
	}

	for _, v := range values {
		switch st.Kind {
		case parser.NavChildren:
			for _, c := range idx.children(v) {
				add(c)
			}
		case parser.NavDescendants:
			for _, d := range idx.descendants(v) {
				add(d)
			}
		case parser.NavParent:
			if p, ok := idx.parentOf(v); ok {
				add(p)
			}
		case parser.NavAncestors:
			for _, a := range idx.ancestors(v) {
				add(a)
			}
		case parser.NavRoot:
			add(idx.root(v))
		}
	}
	return out
}

// buildTraces groups the full span set by trace_id, in first-occurrence
// order (matching group_by's ordering choice), into the `traces` source's
// {trace_id, spans: [...]} shape.
func buildTraces(spans []Value) []Value {
	type trace struct {
		traceID string
		spans   []Value
	}
	byID := map[string]*trace{}
	var order []string

	for _, s := range spans {
		tid := s.Get([]string{"trace_id"}).S
		t, ok := byID[tid]
		if !ok {
			t = &trace{traceID: tid}
			byID[tid] = t
			order = append(order, tid)
		}
		t.spans = append(t.spans, s)
	}

	out := make([]Value, len(order))
	for i, tid := range order {
		t := byID[tid]
		v := NewObject()
		v.Set("trace_id", String(t.traceID))
		v.Set("spans", Array(t.spans))
		out[i] = v
	}
	return out
}
