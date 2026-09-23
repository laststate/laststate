// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const brandPurple = "#7C4DFF"

// Banner returns a compact terminal rendering inspired by the Last State mark.
// The angled white stroke and purple trailing plate remain recognizable while
// fitting normal 80-column terminals.
func Banner() string {
	s := Styles()
	white := lipgloss.NewStyle().Bold(true)
	purple := lipgloss.NewStyle().Bold(true)
	name := lipgloss.NewStyle().Bold(true)
	product := lipgloss.NewStyle().Bold(true)
	if colorEnabled() {
		white = white.Foreground(lipgloss.Color("#F8F8FA"))
		purple = purple.Foreground(lipgloss.Color(brandPurple))
		name = name.Foreground(lipgloss.Color("#F8F8FA"))
		product = product.Foreground(lipgloss.Color(brandPurple))
	}
	logo := strings.Join([]string{
		white.Render("       ╱██████████"),
		white.Render("  █████╱      ╱██") + purple.Render("╱█████"),
		white.Render(" █████      ╱██╱ ") + purple.Render("█████"),
	}, "\n")
	title := strings.Join([]string{
		name.Render("LAST STATE"),
		product.Render("RELAY"),
		s.BannerSub.Render("collect · persist · analyze · forward"),
	}, "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, logo, "    ", title) + "\n"
}

func ShortBanner() string {
	s := Styles()
	relay := lipgloss.NewStyle().Bold(true)
	if colorEnabled() {
		relay = relay.Foreground(lipgloss.Color(brandPurple))
	}
	return s.AppName.Render("LAST STATE") + " " + relay.Render("RELAY") + " " + s.AppTag.Render("· offline-first LEP gateway")
}

func Section(title string) string { return Styles().Section.Render(title) }
func Help(usage string) string    { return Styles().Muted.Render(usage) }
