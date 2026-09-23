// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package ui

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// spinnerModel is the bubbletea program that draws a single animated line.
// The model only ever renders one line so the bubble tea runtime stays
// extremely light: no Init, no Update loop, just a Tick.
type spinnerModel struct {
	spinner  spinner.Model
	action   string
	quitting bool
	err      error
	done     chan struct{}
	result   string
}

func newSpinnerModel(action string) spinnerModel {
	sp := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(Styles().OK),
	)
	return spinnerModel{spinner: sp, action: action, done: make(chan struct{})}
}

func (m spinnerModel) Init() tea.Cmd { return m.spinner.Tick }

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case spinnerDoneMsg:
		m.quitting = true
		m.err = msg.err
		m.result = msg.result
		return m, tea.Quit
	}
	return m, nil
}

func (m spinnerModel) View() string {
	if m.quitting {
		return ""
	}
	return fmt.Sprintf("%s %s", m.spinner.View(), m.action)
}

// spinnerDoneMsg is sent from the runner goroutine to the bubbletea loop to
// signal that the wrapped action finished.
type spinnerDoneMsg struct {
	err    error
	result string
}

// Spinner runs action with an animated spinner prefix on stdout. The
// returned error is the one returned by action. When the output is not a
// TTY the spinner degrades to a single "Working..." line and a follow-up
// success/error line.
func Spinner(action string, fn func() (string, error)) error {
	if !IsTTY() {
		Pending(os.Stdout, action)
		result, err := fn()
		if err != nil {
			Error(os.Stderr, err.Error())
			return err
		}
		if result != "" {
			Success(os.Stdout, result)
		} else {
			Success(os.Stdout, "done")
		}
		return nil
	}

	m := newSpinnerModel(action)
	p := tea.NewProgram(m, tea.WithoutSignalHandler(), tea.WithoutCatchPanics())
	go func() {
		result, err := fn()
		p.Send(spinnerDoneMsg{err: err, result: result})
	}()
	finalModel, err := p.Run()
	if err != nil {
		return err
	}
	fm := finalModel.(spinnerModel)
	if fm.err != nil {
		// Move to a fresh line and emit a styled error so the spinner
		// frame doesn't bleed into the message.
		fmt.Fprintln(os.Stderr)
		Error(os.Stderr, fm.err.Error())
		return fm.err
	}
	fmt.Fprintln(os.Stdout)
	if fm.result != "" {
		Success(os.Stdout, fm.result)
	} else {
		Success(os.Stdout, "done")
	}
	return nil
}

// SpinnerContext is like Spinner but aborts the wrapped function when the
// context is cancelled. The spinner itself is not time-bounded; only the
// caller's work is.
func SpinnerContext(ctx context.Context, action string, fn func(ctx context.Context) (string, error)) error {
	return Spinner(action, func() (string, error) { return fn(ctx) })
}

// SpinnerWithTimeout runs action for at most d, cancelling the underlying
// context on expiry. The spinner keeps spinning until the work returns,
// letting the user see the cancel ripple through the wrapped function.
func SpinnerWithTimeout(ctx context.Context, action string, d time.Duration, fn func(ctx context.Context) (string, error)) error {
	cctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return SpinnerContext(cctx, action, fn)
}
