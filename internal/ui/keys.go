package ui

import (
	"fmt"
	"strconv"
	"strings"
)

type keyAction uint8

const (
	actionHelp keyAction = iota
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
		{actionEscape, []string{"esc"}, "Help", "Close help; outside help, quit"},
		{actionScrollUp, []string{"up"}, "Help", "Scroll help up by one line"},
		{actionScrollDown, []string{"down"}, "Help", "Scroll help down by one line"},
		{actionPageUp, []string{"pgup"}, "Help", "Scroll help up by one page"},
		{actionPageDown, []string{"pgdown"}, "Help", "Scroll help down by one page"},
		{actionView, []string{"p"}, "Views", "Alternate dense and remembered framed view"},
		{actionNextPreset, []string{"P"}, "Views", "Cycle configured framed presets (enter framed from dense)"},
	}
	for _, panel := range panels {
		bindings = append(bindings, keyBinding{actionPanel, []string{strconv.Itoa(panel.hotkey)},
			"Framed panels (session only)", "Toggle " + panel.title})
	}
	return append(bindings,
		keyBinding{actionFaster, []string{"+", "="}, "Refresh (screen only; collector cadence stays the same)", "Refresh faster (halve interval, minimum 100ms)"},
		keyBinding{actionSlower, []string{"-", "_"}, "Refresh (screen only; collector cadence stays the same)", "Refresh slower (double interval, maximum 30000ms)"},
		keyBinding{actionQuit, []string{"q", "ctrl+c"}, "Quit", "Quit, including while help is open"})
}

func bindingForKey(key string) (keyBinding, bool) {
	for _, binding := range keyBindings() {
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
	return fmt.Sprintf("%-15s%s", strings.Join(labels, " / "), b.description)
}
