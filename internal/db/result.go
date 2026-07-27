package db

import "time"

// Result is the outcome of a query or row preview. It is display-oriented: the
// tui layer renders Columns and Rows directly.
//
// Rows holds one slice per row, each cell an any. A nil cell means SQL NULL —
// the renderer must distinguish it from an empty string. Non-nil cells are
// typically string or []byte as returned by the driver; formatting into display
// text happens in the tui layer, not here.
type Result struct {
	Columns      []string
	Rows         [][]any
	RowsAffected int64         // for statements that don't return rows (INSERT/UPDATE/DELETE)
	Duration     time.Duration // wall-clock time the engine spent on the operation
}
