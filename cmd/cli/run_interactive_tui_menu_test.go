package main

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

func newMenuTUI(t *testing.T) *runTUI {
	t.Helper()
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{})
	m.width = 80
	m.height = 24
	m.sessionID = "sess-1"
	m.status = "input"
	m.awaitingInput = true
	m.input.Focus()
	m.cancel = func(context.Context, string) error { return nil }
	m.complete = func(context.Context, string) error { return nil }
	m.layout()
	return m
}

func menuItemIndex(m *runTUI, id string) int {
	for i, it := range m.menu.items {
		if it.id == id {
			return i
		}
	}
	return -1
}

func TestRunTUI_menuOpensAndRendersActions(t *testing.T) {
	m := newMenuTUI(t)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = model.(*runTUI)

	if !m.menu.open {
		t.Fatal("expected menu open after ctrl+p")
	}
	if m.input.Focused() {
		t.Fatal("input should blur while menu open")
	}
	view := m.View()
	for _, want := range []string{"Session actions", "Complete session", "Cancel session", "Detach"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want %q in modal", view, want)
		}
	}
}

func TestRunTUI_menuEscClosesAndRefocusesInput(t *testing.T) {
	m := newMenuTUI(t)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = model.(*runTUI)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(*runTUI)

	if m.menu.open {
		t.Fatal("expected menu closed after esc")
	}
	if !m.input.Focused() {
		t.Fatal("input should refocus after closing menu while awaiting input")
	}
}

func TestRunTUI_menuCompleteInvokesCompleter(t *testing.T) {
	var gotID string
	called := false
	m := newMenuTUI(t)
	m.complete = func(_ context.Context, id string) error {
		called = true
		gotID = id
		return nil
	}

	m.openMenu()
	m.menu.selected = menuItemIndex(m, menuActionComplete)
	model, cmd := m.activateMenuItem()
	m = model.(*runTUI)

	if m.menu.open {
		t.Fatal("menu should close after selecting an action")
	}
	if cmd == nil {
		t.Fatal("expected complete command")
	}
	if m.status != "ending" || !strings.Contains(m.statusHint, "completing") {
		t.Fatalf("status=%q hint=%q, want ending/completing", m.status, m.statusHint)
	}
	if m.sendClosed {
		t.Fatal("complete via RPC must not close the send side")
	}

	msg := cmd()
	if _, ok := msg.(tuiCompleteResult); !ok {
		t.Fatalf("cmd result = %T, want tuiCompleteResult", msg)
	}
	if !called || gotID != "sess-1" {
		t.Fatalf("completer called=%v id=%q, want call with sess-1", called, gotID)
	}
}

func TestRunTUI_menuCompleteFallsBackToCloseSend(t *testing.T) {
	stream := &mockInteractiveClientStream{}
	m := newMenuTUI(t)
	m.stream = stream
	m.complete = nil

	m.openMenu()
	m.menu.selected = menuItemIndex(m, menuActionComplete)
	model, cmd := m.activateMenuItem()
	m = model.(*runTUI)

	if cmd != nil {
		t.Fatalf("fallback complete should not emit a command, got %v", cmd)
	}
	if !m.sendClosed {
		t.Fatal("expected send closed when no completer is wired")
	}
	if m.status != "ending" {
		t.Fatalf("status = %q, want ending", m.status)
	}
}

func TestRunTUI_completeServerMsgClearsEndingState(t *testing.T) {
	m := newMenuTUI(t)
	m.beginEndingAction("completing session…")

	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_Completed{
			Completed: &runtimev1.RunSessionInteractiveCompleted{
				StopReason: "end_turn",
				Stats:      &runtimev1.InteractiveSessionStats{Turn: 1},
			},
		},
	}); err != nil {
		t.Fatalf("completed: %v", err)
	}
	if m.status != "done" {
		t.Fatalf("status = %q, want done", m.status)
	}
	if m.statusHint != "" {
		t.Fatalf("statusHint = %q, want cleared", m.statusHint)
	}
}

func TestRunTUI_cancelServerMsgClearsEndingState(t *testing.T) {
	m := newMenuTUI(t)
	m.beginEndingAction("cancelling session…")

	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_Cancelled{
			Cancelled: &runtimev1.RunSessionInteractiveCancelled{},
		},
	}); err != nil {
		t.Fatalf("cancelled: %v", err)
	}
	if m.status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", m.status)
	}
	if m.statusHint != "" {
		t.Fatalf("statusHint = %q, want cleared", m.statusHint)
	}
}

func TestRunTUI_menuCompleteResultErrorSetsHint(t *testing.T) {
	m := newMenuTUI(t)

	model, _ := m.Update(tuiCompleteResult{err: context.Canceled})
	m = model.(*runTUI)

	if !strings.Contains(m.statusHint, "complete failed") {
		t.Fatalf("statusHint = %q, want complete failure", m.statusHint)
	}
}

func TestRunTUI_menuDetachQuitsWithoutClosingSend(t *testing.T) {
	m := newMenuTUI(t)

	m.openMenu()
	m.menu.selected = menuItemIndex(m, menuActionDetach)
	model, cmd := m.activateMenuItem()
	m = model.(*runTUI)

	if !m.quitting {
		t.Fatal("expected quitting after detach")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command on detach")
	}
	if m.sendClosed {
		t.Fatal("detach must not close the send side")
	}
}

func TestRunTUI_menuCancelInvokesCanceler(t *testing.T) {
	var gotID string
	called := false
	m := newMenuTUI(t)
	m.cancel = func(_ context.Context, id string) error {
		called = true
		gotID = id
		return nil
	}

	m.openMenu()
	m.menu.selected = menuItemIndex(m, menuActionCancel)
	model, cmd := m.activateMenuItem()
	m = model.(*runTUI)

	if cmd == nil {
		t.Fatal("expected cancel command")
	}
	if m.status != "ending" || !strings.Contains(m.statusHint, "cancelling") {
		t.Fatalf("status=%q hint=%q, want ending/cancelling", m.status, m.statusHint)
	}

	msg := cmd()
	if _, ok := msg.(tuiCancelResult); !ok {
		t.Fatalf("cmd result = %T, want tuiCancelResult", msg)
	}
	if !called || gotID != "sess-1" {
		t.Fatalf("canceler called=%v id=%q, want call with sess-1", called, gotID)
	}
}

func TestRunTUI_menuCancelDisabledWithoutCanceler(t *testing.T) {
	m := newMenuTUI(t)
	m.cancel = nil

	m.openMenu()
	idx := menuItemIndex(m, menuActionCancel)
	if idx < 0 {
		t.Fatal("cancel action missing from menu")
	}
	if m.menu.items[idx].enabled {
		t.Fatal("cancel should be disabled without a canceler")
	}
	if m.menu.selected == idx {
		t.Fatal("disabled cancel item should not be the default selection")
	}
}

func TestRunTUI_menuCancelResultErrorSetsHint(t *testing.T) {
	m := newMenuTUI(t)

	model, _ := m.Update(tuiCancelResult{err: context.Canceled})
	m = model.(*runTUI)

	if !strings.Contains(m.statusHint, "cancel failed") {
		t.Fatalf("statusHint = %q, want cancel failure", m.statusHint)
	}
}

func TestRunTUI_menuTerminalSessionOffersClose(t *testing.T) {
	m := newMenuTUI(t)
	m.readOnly = true
	m.status = "done"

	m.openMenu()
	if len(m.menu.items) != 1 || m.menu.items[0].id != menuActionQuit {
		t.Fatalf("terminal menu = %+v, want single close action", m.menu.items)
	}
	model, cmd := m.activateMenuItem()
	m = model.(*runTUI)
	if !m.quitting || cmd == nil {
		t.Fatal("expected quit after closing terminal session viewer")
	}
}
