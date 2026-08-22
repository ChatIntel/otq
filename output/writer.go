// Package output writes query results to a stream. Default output is JSONL
// (one JSON value per line, no wrapping array) so `otq ... | jq ...`
// composes correctly downstream, matching jq's own streaming behavior.
package output

import (
	"encoding/json"
	"fmt"
	"io"

	"otq/internal/eval"
)

type Writer struct {
	W      io.Writer
	Raw    bool
	Pretty bool
}

// WriteValue writes one result value, followed by a newline.
//
// --raw only changes rendering for scalar string results (matches `jq -r`
// exactly): non-string values are unaffected and always JSON-encoded.
func (w Writer) WriteValue(v eval.Value) error {
	if w.Raw && v.Kind == eval.KString {
		_, err := fmt.Fprintln(w.W, v.S)
		return err
	}

	var data []byte
	var err error
	if w.Pretty {
		data, err = json.MarshalIndent(v.ToJSON(), "", "  ")
	} else {
		data, err = json.Marshal(v.ToJSON())
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w.W, string(data))
	return err
}

// WriteAll writes each value in order, one per line.
func (w Writer) WriteAll(values []eval.Value) error {
	for _, v := range values {
		if err := w.WriteValue(v); err != nil {
			return err
		}
	}
	return nil
}
