package ui

import (
	"bytes"
	"testing"
	"time"
)

func TestTimestamp(t *testing.T) {
	ts := timestamp(time.Date(2024, 6, 15, 14, 30, 45, 0, time.UTC))
	if ts != "14:30:45" {
		t.Errorf("unexpected timestamp: %s", ts)
	}
}

func TestStripAnsi(t *testing.T) {
	input := "\x1b[31mred\x1b[0m text"
	got := stripAnsi(input)
	if got != "red text" {
		t.Errorf("expected 'red text', got %q", got)
	}
}

func TestJoinVertical(t *testing.T) {
	result := joinVertical("a", "b", "", "c")
	if result != "a\n\nb\n\nc" {
		t.Errorf("unexpected join: %q", result)
	}
}

func TestJoinVerticalEmpty(t *testing.T) {
	result := joinVertical("", "  ", "a")
	if result != "a" {
		t.Errorf("expected 'a', got %q", result)
	}
}

func TestDivider(t *testing.T) {
	d := divider(20)
	if len(d) != 20 {
		t.Errorf("expected 20 chars, got %d", len(d))
	}
}

func TestWriteLine(t *testing.T) {
	var buf bytes.Buffer
	writeLine(&buf, "hello")
	if buf.String() != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", buf.String())
	}
}

func TestRenderLines(t *testing.T) {
	var buf bytes.Buffer
	renderLines(&buf, "line1\nline2")
	if !bytes.Contains(buf.Bytes(), []byte("line1")) {
		t.Error("should contain line1")
	}
}

func TestKeyValue(t *testing.T) {
	result := keyValue("status", "ok")
	if result == "" {
		t.Error("keyValue should not be empty")
	}
}

func TestCard(t *testing.T) {
	c := card("Title", "content", 40)
	if c == "" {
		t.Error("card should not be empty")
	}
}

func TestCardNoTitle(t *testing.T) {
	c := card("", "content", 0)
	if c == "" {
		t.Error("card should not be empty")
	}
}

func TestStyles(t *testing.T) {
	s := Styles()
	// lipgloss.Style is a struct, not nilable - verify it produces output
	key := s.Key.Render("test")
	if key == "" {
		t.Error("Key style should produce output")
	}
	val := s.Value.Render("test")
	if val == "" {
		t.Error("Value style should produce output")
	}
	card := s.Card.Render("test")
	if card == "" {
		t.Error("Card style should produce output")
	}
}
