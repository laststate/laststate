// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package ui

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Column describes one column in a Table. Width zero means "expand to fit
// the content"; fixed widths are honored verbatim.
type Column struct {
	Title string
	Width int
	Align string // "left" (default), "right", "center"
}

// Row is a list of cell strings. Cells are styled with the same rules as
// the table itself, so callers don't have to apply color themselves.
type Row []string

// Table renders a list of rows into a styled grid. The table respects the
// color-disabled fallback and degrades to a columnar plain-text layout.
type Table struct {
	columns []Column
	rows    []Row
	header  string
}

// NewTable returns a Table with the supplied columns. The table is empty
// until rows are appended.
func NewTable(columns ...Column) *Table {
	cp := make([]Column, len(columns))
	copy(cp, columns)
	return &Table{columns: cp}
}

// Header sets an optional title rendered above the table.
func (t *Table) Header(title string) *Table {
	t.header = title
	return t
}

// AppendRow adds a row to the table. Rows shorter than the column count are
// padded with empty strings; rows longer than the column count are truncated.
func (t *Table) AppendRow(cells ...string) *Table {
	row := make(Row, len(t.columns))
	for i := range row {
		if i < len(cells) {
			row[i] = cells[i]
		}
	}
	t.rows = append(t.rows, row)
	return t
}

// AppendRows adds multiple rows at once.
func (t *Table) AppendRows(rows ...Row) *Table {
	t.rows = append(t.rows, rows...)
	return t
}

// Render writes the table to w as a complete block. The block ends with a
// newline so the next print starts on a fresh line.
func (t *Table) Render(w io.Writer) {
	if w == nil {
		w = defaultWriter()
	}
	if !colorEnabled() {
		renderLines(w, t.plain())
		return
	}
	renderLines(w, t.styled())
}

// String returns the table as a string. Useful for embedding in panels.
func (t *Table) String() string {
	if !colorEnabled() {
		return t.plain()
	}
	return t.styled()
}

func (t *Table) columnWidths() []int {
	widths := make([]int, len(t.columns))
	for i, c := range t.columns {
		if c.Width > 0 {
			widths[i] = c.Width
			continue
		}
		// Auto: pick the widest cell or the title, whichever wins.
		max := utf8.RuneCountInString(c.Title)
		for _, r := range t.rows {
			if i < len(r) {
				w := utf8.RuneCountInString(r[i])
				if w > max {
					max = w
				}
			}
		}
		widths[i] = max
	}
	return widths
}

func (t *Table) plain() string {
	widths := t.columnWidths()
	var b strings.Builder
	if t.header != "" {
		b.WriteString(t.header)
		b.WriteString("\n")
	}
	// Header row
	for i, c := range t.columns {
		if i > 0 {
			b.WriteString("    ")
		}
		b.WriteString(padRight(c.Title, widths[i]))
	}
	b.WriteString("\n")
	for i := range t.columns {
		if i > 0 {
			b.WriteString("    ")
		}
		b.WriteString(strings.Repeat("-", widths[i]))
	}
	b.WriteString("\n")
	for _, r := range t.rows {
		for i := range t.columns {
			if i > 0 {
				b.WriteString("    ")
			}
			cell := ""
			if i < len(r) {
				cell = r[i]
			}
			b.WriteString(padRight(cell, widths[i]))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (t *Table) styled() string {
	s := Styles()
	widths := t.columnWidths()
	var b strings.Builder
	if t.header != "" {
		b.WriteString(s.CardTitle.Render(t.header))
		b.WriteString("\n")
	}
	for i, c := range t.columns {
		if i > 0 {
			b.WriteString("    ")
		}
		b.WriteString(s.TableHead.Render(padRight(c.Title, widths[i])))
	}
	b.WriteString("\n")
	for i := range t.columns {
		if i > 0 {
			b.WriteString("    ")
		}
		b.WriteString(s.TableDim.Render(strings.Repeat("·", widths[i])))
	}
	b.WriteString("\n")
	for _, r := range t.rows {
		for i := range t.columns {
			if i > 0 {
				b.WriteString("    ")
			}
			cell := ""
			if i < len(r) {
				cell = r[i]
			}
			b.WriteString(s.TableCell.Render(padRight(cell, widths[i])))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func padRight(s string, width int) string {
	w := utf8.RuneCountInString(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// KVTable renders a two-column key/value listing. The table reuses the
// styling rules of the regular Table but always aligns the keys to the left
// and the values to the right of the column.
func KVTable(pairs [][2]string) string {
	t := NewTable(Column{Title: "Key", Width: 16}, Column{Title: "Value"})
	for _, p := range pairs {
		t.AppendRow(p[0], p[1])
	}
	return t.String()
}

// RenderKVTable is a convenience helper that prints a key/value table to w.
func RenderKVTable(w io.Writer, pairs [][2]string) { fmt.Fprint(w, KVTable(pairs)) }
