// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk went away") }

func TestValidateReportFormat(t *testing.T) {
	for _, format := range []string{"", FormatText, FormatJSON} {
		if err := ValidateReportFormat(format); err != nil {
			t.Errorf("ValidateReportFormat(%q) = %v, want nil", format, err)
		}
	}

	// A misspelling used to fall through to text, so a pipeline asking for JSON
	// silently got a table to parse.
	for _, format := range []string{"jsonl", "JSON", "yaml", "Text", "  json"} {
		err := ValidateReportFormat(format)
		if err == nil {
			t.Errorf("ValidateReportFormat(%q) = nil, want an error", format)
			continue
		}

		if !strings.Contains(err.Error(), format) {
			t.Errorf("ValidateReportFormat(%q) error %q does not name the format", format, err)
		}
	}
}

func TestWriteReport_JSONIgnoresTheTextWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	report := struct {
		Pass bool `json:"pass"`
	}{Pass: true}

	err := WriteReport(FormatJSON, report, func(io.Writer) {
		t.Fatal("the text writer ran for --format json")
	}, buf)
	if err != nil {
		t.Fatalf("WriteReport failed: %v", err)
	}

	if !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("JSON output does not end in a newline: %q", buf.String())
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, buf.String())
	}

	if decoded["pass"] != true {
		t.Errorf("decoded pass = %v, want true", decoded["pass"])
	}
}

func TestWriteReport_TextAndEmptyFormatUseTheTextWriter(t *testing.T) {
	for _, format := range []string{FormatText, ""} {
		buf := &bytes.Buffer{}

		err := WriteReport(format, struct{}{}, func(w io.Writer) {
			_, _ = io.WriteString(w, "rendered")
		}, buf)
		if err != nil {
			t.Fatalf("WriteReport(%q) failed: %v", format, err)
		}

		if buf.String() != "rendered" {
			t.Errorf("WriteReport(%q) wrote %q, want %q", format, buf.String(), "rendered")
		}
	}
}

func TestWriteReport_RejectsAnUnknownFormatWithoutWritingAnything(t *testing.T) {
	buf := &bytes.Buffer{}

	err := WriteReport("yaml", struct{}{}, func(io.Writer) {
		t.Fatal("the text writer ran for an unsupported format")
	}, buf)
	if err == nil {
		t.Fatal("WriteReport(\"yaml\") = nil, want an error")
	}

	if buf.Len() != 0 {
		t.Errorf("WriteReport wrote %q for an unsupported format", buf.String())
	}
}

func TestWriteReport_ReportsAFailedWrite(t *testing.T) {
	// The report is the whole product of a drill. A write that silently half
	// succeeded would leave an operator reading a truncated verdict.
	err := WriteReport(FormatJSON, struct{}{}, nil, failingWriter{})
	if err == nil {
		t.Fatal("WriteReport = nil for a failing writer, want an error")
	}

	if !strings.Contains(err.Error(), "disk went away") {
		t.Errorf("error %q does not wrap the underlying write failure", err)
	}
}

func TestWriteReport_ReportsAnUnencodableReport(t *testing.T) {
	err := WriteReport(FormatJSON, struct{ Fn func() }{Fn: func() {}}, nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("WriteReport = nil for an unencodable report, want an error")
	}
}
