// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package ui

import (
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
)

// palette groups the colors used across the package. The default theme reads
// like a calm lab tool: deep slate background, electric cyan accents, warm
// amber for warnings, and a soft red for failures.
type palette struct {
	brand    lipgloss.Color
	brandAlt lipgloss.Color
	muted    lipgloss.Color
	text     lipgloss.Color
	dim      lipgloss.Color
	ok       lipgloss.Color
	warn     lipgloss.Color
	err      lipgloss.Color
	info     lipgloss.Color
	pending  lipgloss.Color
	dead     lipgloss.Color
	accent   lipgloss.Color
	success  lipgloss.Color
	bg       lipgloss.Color
}

func defaultPalette() palette {
	return palette{
		brand:    lipgloss.Color("#7C4DFF"), // Last State purple
		brandAlt: lipgloss.Color("#9A6BFF"), // purple highlight
		muted:    lipgloss.Color("#5C6B7A"),
		text:     lipgloss.Color("#E6EDF3"),
		dim:      lipgloss.Color("#8B98A5"),
		ok:       lipgloss.Color("#7AE582"),
		warn:     lipgloss.Color("#F5C26B"),
		err:      lipgloss.Color("#FF6B6B"),
		info:     lipgloss.Color("#9AD1FF"),
		pending:  lipgloss.Color("#F5C26B"),
		dead:     lipgloss.Color("#FF6B6B"),
		accent:   lipgloss.Color("#7C4DFF"),
		success:  lipgloss.Color("#7AE582"),
		bg:       lipgloss.Color("#0F1620"),
	}
}

// styles groups every pre-built lipgloss style. Layouts are kept here so that
// callers don't rebuild them on every render.
type styles struct {
	p palette

	AppName   lipgloss.Style
	AppTag    lipgloss.Style
	Section   lipgloss.Style
	Card      lipgloss.Style
	CardTitle lipgloss.Style
	Key       lipgloss.Style
	Value     lipgloss.Style
	Muted     lipgloss.Style
	OK        lipgloss.Style
	Warn      lipgloss.Style
	Err       lipgloss.Style
	Info      lipgloss.Style
	Pending   lipgloss.Style
	Dead      lipgloss.Style
	Bold      lipgloss.Style
	Footer    lipgloss.Style
	Banner    lipgloss.Style
	BannerSub lipgloss.Style
	Divider   lipgloss.Style
	TableHead lipgloss.Style
	TableCell lipgloss.Style
	TableDim  lipgloss.Style
	Stat      lipgloss.Style
	StatLabel lipgloss.Style
	StatValue lipgloss.Style
}

func buildStyles() *styles {
	p := defaultPalette()
	s := &styles{p: p}

	s.AppName = lipgloss.NewStyle().Foreground(p.brand).Bold(true)
	s.AppTag = lipgloss.NewStyle().Foreground(p.dim)
	s.Section = lipgloss.NewStyle().
		Foreground(p.brand).
		Bold(true).
		Padding(0, 1).
		BorderLeft(true).
		BorderForeground(p.brandAlt)
	s.Card = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.muted).
		Padding(0, 1)
	s.CardTitle = lipgloss.NewStyle().Foreground(p.brand).Bold(true)
	s.Key = lipgloss.NewStyle().Foreground(p.dim)
	s.Value = lipgloss.NewStyle().Foreground(p.text).Bold(true)
	s.Muted = lipgloss.NewStyle().Foreground(p.muted)
	s.OK = lipgloss.NewStyle().Foreground(p.ok)
	s.Warn = lipgloss.NewStyle().Foreground(p.warn)
	s.Err = lipgloss.NewStyle().Foreground(p.err)
	s.Info = lipgloss.NewStyle().Foreground(p.info)
	s.Pending = lipgloss.NewStyle().Foreground(p.pending)
	s.Dead = lipgloss.NewStyle().Foreground(p.dead)
	s.Bold = lipgloss.NewStyle().Bold(true).Foreground(p.text)
	s.Footer = lipgloss.NewStyle().Foreground(p.muted)
	s.Banner = lipgloss.NewStyle().Foreground(p.brand).Bold(true)
	s.BannerSub = lipgloss.NewStyle().Foreground(p.dim).Italic(true)
	s.Divider = lipgloss.NewStyle().Foreground(p.muted)
	s.TableHead = lipgloss.NewStyle().Foreground(p.brand).Bold(true)
	s.TableCell = lipgloss.NewStyle().Foreground(p.text)
	s.TableDim = lipgloss.NewStyle().Foreground(p.dim)
	s.Stat = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.muted).
		Padding(0, 1).
		Width(18).
		Align(lipgloss.Center)
	s.StatLabel = lipgloss.NewStyle().Foreground(p.dim)
	s.StatValue = lipgloss.NewStyle().Foreground(p.text).Bold(true)

	return s
}

// shared style instance, populated lazily on first access.
var sharedStyles atomic.Pointer[styles]

// Styles returns the package-wide style table. The pointer is cached so that
// every render path uses the same styles without recomputing them.
func Styles() *styles {
	if s := sharedStyles.Load(); s != nil {
		return s
	}
	s := buildStyles()
	sharedStyles.Store(s)
	return s
}
