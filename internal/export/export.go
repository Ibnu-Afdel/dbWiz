// Package export renders a query result — a column header plus rows of cells — to
// the two portable formats DBWiz can write out: CSV and JSON. It is a leaf
// package (no internal imports) so both the TUI's "export results" action (v2
// 2.3) and, later, the scripting surface (v2 Phase 4, `dbwiz query --json`) can
// share one definition of how a result serialises.
//
// A cell is an `any`, matching db.Result.Rows: a nil cell is SQL NULL and is
// rendered distinctly from an empty string (an empty CSV field is not the same
// as NULL, and JSON null is not "" ). Non-nil cells are typically string or
// []byte as the drivers return them; anything else is formatted with fmt.Sprint
// for CSV and passed through to encoding/json for JSON.
package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
)

// CSV renders the grid as RFC 4180 CSV: a header row of column names followed by
// one row per record. A NULL cell becomes an empty field. []byte and string
// cells are written verbatim; other types are formatted with fmt.Sprint.
func CSV(columns []string, rows [][]any) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(columns); err != nil {
		return nil, err
	}
	rec := make([]string, len(columns))
	for _, row := range rows {
		for i := range columns {
			if i < len(row) {
				rec[i] = csvCell(row[i])
			} else {
				rec[i] = ""
			}
		}
		if err := w.Write(rec); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// JSON renders the grid as a pretty-printed array of row objects keyed by column
// name — the shape `jq` and most consumers expect. A NULL cell becomes JSON
// null; []byte and other cells are normalised to their natural JSON type via
// jsonCell. An empty result is a valid empty array, not null.
func JSON(columns []string, rows [][]any) ([]byte, error) {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		obj := make(map[string]any, len(columns))
		for i, col := range columns {
			if i < len(row) {
				obj[col] = jsonCell(row[i])
			} else {
				obj[col] = nil
			}
		}
		out = append(out, obj)
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// csvCell stringifies one cell for CSV. nil (SQL NULL) is an empty field.
func csvCell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

// jsonCell normalises one cell for JSON. nil stays nil (→ null); []byte becomes a
// string (the drivers hand back text as bytes); everything else is passed through
// for encoding/json to type naturally.
func jsonCell(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	default:
		return t
	}
}
