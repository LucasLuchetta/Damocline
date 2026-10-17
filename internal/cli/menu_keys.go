package cli

import (
	"fmt"
)

// Interactive key navigation only engages when a real console can provide
// individual key events and redraw control. Redirected input keeps numeric
// prompts so scripts, tests and CI continue to work without a TTY.
type homeKey string

const (
	homeKeyUp     homeKey = "up"
	homeKeyDown   homeKey = "down"
	homeKeyEnter  homeKey = "enter"
	homeKeyEscape homeKey = "escape"
)

type homeKeyboard interface {
	readKey() (homeKey, error)
	close()
}

func selectWelcomeMenu(w *wizard, hasCurrent, color, interactive bool) (string, error) {
	if interactive {
		if keyboard, ok := openHomeKeyboard(); ok {
			defer keyboard.close()
			return welcomeMenuWithKeyboard(w, keyboard, hasCurrent, color)
		}
	}
	return welcomeMenu(w, hasCurrent, color)
}

func homeIndexForDigit(key homeKey) (int, bool) {
	switch key {
	case "1", "2", "3", "4":
		return int(key[0] - '1'), true
	case "0":
		return 4, true
	default:
		return 0, false
	}
}

func renderKeyboardHome(w *wizard, hasCurrent, color bool, selected int, status string) int {
	options := homeMenuOptions(hasCurrent)
	lines := []string{"  What would you like to do?", ""}
	for i, option := range options {
		indicator := " "
		if i == selected {
			indicator = ">"
		}
		line := fmt.Sprintf("  %s [%s] %-19s %s", indicator, option.key, option.label, option.detail)
		if color && i == selected {
			line = fmt.Sprintf("  \x1b[1;97m%s [%s] %-19s\x1b[0m \x1b[90m%s\x1b[0m", indicator, option.key, option.label, option.detail)
		} else if color {
			line = fmt.Sprintf("  %s [%s] %-19s \x1b[90m%s\x1b[0m", indicator, option.key, option.label, option.detail)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", "  ↑/↓ Navigate   Enter Select   0-4 Quick select", "  "+status)
	for _, line := range lines {
		// Clear the previous rendition before writing, including when a status
		// message is shorter than its predecessor.
		fmt.Fprintf(w.out, "\r\x1b[2K%s\n", line)
	}
	return len(lines)
}

func welcomeMenuWithKeyboard(w *wizard, keyboard homeKeyboard, hasCurrent, color bool) (string, error) {
	selected, status := 0, ""
	lines := renderKeyboardHome(w, hasCurrent, color, selected, status)
	for {
		key, err := keyboard.readKey()
		if err != nil {
			return "", err
		}
		activate := false
		switch key {
		case homeKeyUp:
			selected = (selected + 4) % 5
			status = ""
		case homeKeyDown:
			selected = (selected + 1) % 5
			status = ""
		case homeKeyEnter:
			activate = true
		case homeKeyEscape:
			return "exit", nil
		default:
			if digit, ok := homeIndexForDigit(key); ok {
				selected = digit
				activate = true
			} else {
				continue
			}
		}
		if activate {
			if selected != 2 || hasCurrent {
				return []string{"new", "history", "current", "help", "exit"}[selected], nil
			}
			status = "No active scenario. Choose [1] to create one."
		}
		// Move to the beginning of the printed menu and redraw the highlight.
		fmt.Fprintf(w.out, "\x1b[%dF", lines)
		lines = renderKeyboardHome(w, hasCurrent, color, selected, status)
	}
}
