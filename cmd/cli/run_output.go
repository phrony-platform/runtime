package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// completionWriter streams plain text immediately and buffers JSON-looking completions
// so the CLI can print indented JSON once the turn finishes.
type completionWriter struct {
	out  io.Writer
	buf  strings.Builder
	mode completionOutputMode
}

type completionOutputMode int

const (
	completionModeUnknown completionOutputMode = iota
	completionModePlain
	completionModeJSON
)

func newCompletionWriter(out io.Writer) *completionWriter {
	return &completionWriter{out: out}
}

func (w *completionWriter) WriteDelta(delta string) error {
	if delta == "" {
		return nil
	}
	w.buf.WriteString(delta)

	switch w.mode {
	case completionModePlain:
		_, err := io.WriteString(w.out, delta)
		w.buf.Reset()
		return err
	case completionModeJSON:
		return nil
	default:
		trimmed := strings.TrimSpace(w.buf.String())
		if trimmed == "" {
			return nil
		}
		if trimmed[0] == '{' || trimmed[0] == '[' {
			w.mode = completionModeJSON
			return nil
		}
		w.mode = completionModePlain
		_, err := io.WriteString(w.out, w.buf.String())
		w.buf.Reset()
		return err
	}
}

func (w *completionWriter) Flush() error {
	defer w.reset()
	if w.buf.Len() == 0 {
		return nil
	}
	raw := []byte(w.buf.String())
	if w.mode == completionModeJSON || (w.mode == completionModeUnknown && json.Valid(raw)) {
		if _, err := io.WriteString(w.out, "\n"); err != nil {
			return err
		}
		return writePrettifiedJSON(w.out, raw)
	}
	_, err := io.WriteString(w.out, w.buf.String())
	return err
}

func (w *completionWriter) reset() {
	w.buf.Reset()
	w.mode = completionModeUnknown
}

func writePrettifiedJSON(w io.Writer, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		_, err := w.Write(raw)
		return err
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		_, err := w.Write(raw)
		return err
	}
	_, err = w.Write(pretty)
	return err
}

// prettifySessionOutput formats session output JSON for the terminal. When the
// envelope's message field is itself JSON text, it is parsed and indented inline.
func prettifySessionOutput(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if !json.Valid(raw) {
		return raw, nil
	}

	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return indentJSONBytes(raw)
	}

	if msg, ok := envelope["message"].(string); ok && json.Valid([]byte(msg)) {
		var nested any
		if err := json.Unmarshal([]byte(msg), &nested); err == nil {
			envelope["message"] = nested
		}
	}
	return indentJSONValue(envelope)
}

func indentJSONBytes(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw, nil
	}
	return indentJSONValue(v)
}

func indentJSONValue(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
