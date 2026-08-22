package output

import (
	"bytes"
	"strings"
	"testing"

	"otq/internal/eval"
)

func TestWriter_DefaultJSONL_NoWrappingArray(t *testing.T) {
	var buf bytes.Buffer
	w := Writer{W: &buf}
	values := []eval.Value{eval.String("a"), eval.Number(1)}
	if err := w.WriteAll(values); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	want := "\"a\"\n1\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.HasPrefix(got, "[") {
		t.Error("output must not be wrapped in an array")
	}
}

func TestWriter_Raw_StringOnly(t *testing.T) {
	var buf bytes.Buffer
	w := Writer{W: &buf, Raw: true}
	if err := w.WriteValue(eval.String("hello")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hello\n" {
		t.Errorf("got %q, want %q (no quotes)", buf.String(), "hello\n")
	}
}

func TestWriter_Raw_NonStringUnaffected(t *testing.T) {
	var buf bytes.Buffer
	w := Writer{W: &buf, Raw: true}
	if err := w.WriteValue(eval.Number(42)); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "42\n" {
		t.Errorf("got %q, want %q (raw has no effect on non-strings)", buf.String(), "42\n")
	}
}

func TestWriter_Pretty(t *testing.T) {
	var buf bytes.Buffer
	w := Writer{W: &buf, Pretty: true}
	v := eval.NewObject()
	v.Set("a", eval.Number(1))
	if err := w.WriteValue(v); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\n  \"a\"") {
		t.Errorf("pretty output not indented: %q", buf.String())
	}
}

func TestWriter_KeyOrderPreserved(t *testing.T) {
	var buf bytes.Buffer
	w := Writer{W: &buf}
	v := eval.NewObject()
	v.Set("b", eval.String("x"))
	v.Set("a", eval.String("y"))
	if err := w.WriteValue(v); err != nil {
		t.Fatal(err)
	}
	want := `{"b":"x","a":"y"}` + "\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q (project field order preserved, not alphabetical)", buf.String(), want)
	}
}
