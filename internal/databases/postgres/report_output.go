// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"encoding/json"
	"fmt"
	"io"
)

// The output formats every fork report command accepts.
const (
	FormatText = "text"
	FormatJSON = "json"
)

// ValidateReportFormat rejects a format no report can be rendered in.
//
// Commands call this while parsing flags rather than leaving it to render time.
// restore-test and neon-drill only reach their report after an hour of
// restoring, and learning there that --format was misspelled wastes the run.
//
// An empty format means the caller never set one, and gets text.
func ValidateReportFormat(format string) error {
	switch format {
	case "", FormatText, FormatJSON:
		return nil
	default:
		return fmt.Errorf("unsupported --format %q: expected %s or %s", format, FormatText, FormatJSON)
	}
}

// WriteReport renders report as JSON, or hands off to writeText.
//
// Every report command shares this so that --format json means the same bytes
// everywhere, and so an unrenderable format is an error rather than a silent
// fallback to text: a pipeline asking for JSON should hear about the typo
// instead of being handed a table to parse.
func WriteReport(format string, report any, writeText func(io.Writer), output io.Writer) error {
	if err := ValidateReportFormat(format); err != nil {
		return err
	}

	if format != FormatJSON {
		writeText(output)

		return nil
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("could not encode the report as JSON: %w", err)
	}

	if _, err := output.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("could not write the report: %w", err)
	}

	return nil
}
