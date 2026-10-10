package ui

import (
	"fmt"
	"strconv"
	"strings"
)

type keyAction uint8

const (
	actionHelp keyAction = iota
	actionFilter
	actionFilterEditor
	actionDetails
	actionEscape
	actionScrollUp
	actionScrollDown
	actionPageUp
	actionPageDown
	actionView
	actionNextPreset
	actionPanel
	actionFaster
	actionSlower
	actionQuit
)

// One binding names both the accepted keys and the action help explains.
// Panel bindings come from the panel table so their digits cannot drift from
// the hotkeys drawn in the frames.
type keyBinding struct {
	action      keyAction
	keys        []string
	group       string
	description string
}

func keyBindings() []keyBinding {
	bindings := []keyBinding{
		{actionHelp, []string{"?", "h"}, "Help", "Open or close help"},
		{actionEscape, []string{"esc"}, "Help", "Close help/details; clear filter; otherwise quit"},
		{actionScrollUp, []string{"up"}, "Help", "Select previous subject; scroll help/details up"},
		{actionScrollDown, []string{"down"}, "Help", "Select next subject; scroll help/details down"},
		{actionPageUp, []string{"pgup"}, "Help", "Scroll help/details up by one page"},
		{actionPageDown, []string{"pgdown"}, "Help", "Scroll help/details down by one page"},
		{actionDetails, []string{"enter"}, "Selection", "Open details for selected subject"},
		{actionScrollDown, []string{"j", "k"}, "Vim keys (--vim-keys only)", "Down / Up aliases (h remains help)"},
		{actionView, []string{"p"}, "Views", "Alternate dense and remembered framed view"},
		{actionNextPreset, []string{"P"}, "Views", "Cycle configured framed presets (enter framed from dense)"},
	}
	for _, panel := range panels {
		bindings = append(bindings, keyBinding{actionPanel, []string{strconv.Itoa(panel.hotkey)},
			"Framed panels (session only)", "Toggle " + panel.title})
	}
	bindings = append(bindings,
		keyBinding{actionFilter, []string{"/"}, "Filtering", "Edit subject filter (monitor only)"},
		keyBinding{actionFilterEditor, []string{"Enter / Esc"}, "Filter editing", "confirm / cancel preview; Esc restores selection"},
		keyBinding{actionFilterEditor, []string{"Left / Right"}, "Filter editing", "Move cursor by character"},
		keyBinding{actionFilterEditor, []string{"Home / End"}, "Filter editing", "Move cursor to beginning / end"},
		keyBinding{actionFilterEditor, []string{"Backspace / Delete"}, "Filter editing", "Delete previous / next character"},
		keyBinding{actionFilterEditor, []string{"Ctrl+U"}, "Filter editing", "Clear text; Ctrl+C quits"},
		keyBinding{actionFilterEditor, []string{"Printable / paste"}, "Filter editing", "Insert literal text (including q/h/j/k); Up/Down suspended"})
	return append(bindings,
		keyBinding{actionFaster, []string{"+", "="}, "Refresh (screen only; collector cadence stays the same)", "Refresh faster (halve interval, minimum 100ms)"},
		keyBinding{actionSlower, []string{"-", "_"}, "Refresh (screen only; collector cadence stays the same)", "Refresh slower (double interval, maximum 30000ms)"},
		keyBinding{actionQuit, []string{"q", "ctrl+c"}, "Quit", "Quit from monitor, help or details"})
}

func bindingForKey(key string) (keyBinding, bool) {
	for _, binding := range keyBindings() {
		if binding.action == actionFilterEditor || binding.group == "Vim keys (--vim-keys only)" {
			continue
		}
		for _, accepted := range binding.keys {
			if accepted == key {
				return binding, true
			}
		}
	}
	return keyBinding{}, false
}

func (b keyBinding) helpText() string {
	labels := make([]string, len(b.keys))
	for i, key := range b.keys {
		switch key {
		case "esc":
			labels[i] = "Esc"
		case "ctrl+c":
			labels[i] = "Ctrl+C"
		case "pgup":
			labels[i] = "PgUp"
		case "pgdown":
			labels[i] = "PgDn"
		case "up", "down":
			labels[i] = strings.ToUpper(key[:1]) + key[1:]
		default:
			labels[i] = key
		}
	}
	label := strings.Join(labels, " / ")
	return fmt.Sprintf("%-*s%s", max(15, len(label)+1), label, b.description)
}
