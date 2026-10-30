package interactive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/camem2442/keycli/internal/resolve"
	"golang.org/x/term"
)

var ErrCanceled = errors.New("canceled by user")

// MatchItem checks if item matches query (case-insensitive substring on Name, Aliases, Note).
func MatchItem(item resolve.ListItem, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(item.Name), q) {
		return true
	}
	for _, alias := range item.Aliases {
		if strings.Contains(strings.ToLower(alias), q) {
			return true
		}
	}
	if strings.Contains(strings.ToLower(item.Note), q) {
		return true
	}
	return false
}

// FilterItems returns items matching query.
func FilterItems(items []resolve.ListItem, query string) []resolve.ListItem {
	if query == "" {
		return items
	}
	var res []resolve.ListItem
	for _, it := range items {
		if MatchItem(it, query) {
			res = append(res, it)
		}
	}
	return res
}

// PickSecret opens an interactive fuzzy search & selection prompt.
func PickSecret(ctx context.Context, items []resolve.ListItem, promptTitle string) (*resolve.ListItem, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("등록된 시크릿이 없습니다")
	}
	if !IsTerminal(os.Stdin) {
		return nil, fmt.Errorf("대화형 선택은 터미널에서만 지원됩니다")
	}

	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	defer term.Restore(fd, oldState)
	defer fmt.Fprint(os.Stderr, ShowCursor)

	fmt.Fprint(os.Stderr, HideCursor)

	query := ""
	cursor := 0
	maxDisplay := 8

	renderLines := 0

	clearRender := func() {
		if renderLines > 0 {
			// Move cursor up renderLines and clear down
			fmt.Fprintf(os.Stderr, "\033[%dA\r\033[J", renderLines)
			renderLines = 0
		}
	}

	for {
		filtered := FilterItems(items, query)
		if cursor >= len(filtered) {
			if len(filtered) > 0 {
				cursor = len(filtered) - 1
			} else {
				cursor = 0
			}
		}
		if cursor < 0 {
			cursor = 0
		}

		clearRender()

		// Calculate window for scrolling
		start := 0
		if cursor >= maxDisplay {
			start = cursor - maxDisplay + 1
		}
		end := start + maxDisplay
		if end > len(filtered) {
			end = len(filtered)
		}

		var lines []string

		title := promptTitle
		if title == "" {
			title = "시크릿 선택"
		}
		lines = append(lines, fmt.Sprintf("%s%s%s (총 %d개)", Bold, title, Reset, len(filtered)))
		lines = append(lines, fmt.Sprintf("%s검색:%s %s%s%s", Cyan, Reset, Yellow, query, Reset))

		if len(filtered) == 0 {
			lines = append(lines, fmt.Sprintf("  %s일치하는 시크릿이 없습니다%s", Gray, Reset))
		} else {
			for i := start; i < end; i++ {
				it := filtered[i]
				isSelected := (i == cursor)
				prefix := "  "
				nameStyle := ""
				resetStyle := ""

				if isSelected {
					prefix = fmt.Sprintf("%s❯%s ", Cyan, Reset)
					nameStyle = fmt.Sprintf("%s%s", Bold, Cyan)
					resetStyle = Reset
				}

				extra := ""
				if it.Backend != "" {
					extra += fmt.Sprintf(" %s[%s]%s", Gray, it.Backend, Reset)
				}
				if it.Note != "" {
					extra += fmt.Sprintf(" %s(%s)%s", Dim, TruncateString(it.Note, 25), Reset)
				}
				if it.PolicyDenied {
					extra += fmt.Sprintf(" %s[policy]%s", Red, Reset)
				}

				line := fmt.Sprintf("%s%s%s%s%s", prefix, nameStyle, it.Name, resetStyle, extra)
				lines = append(lines, line)
			}
		}

		lines = append(lines, fmt.Sprintf("%s[↑/↓/j/k] 이동  [Enter] 선택  [Esc/Ctrl+C] 취소%s", Gray, Reset))

		for _, l := range lines {
			fmt.Fprintf(os.Stderr, "%s\r\n", l)
		}
		renderLines = len(lines)

		kp, err := ReadKey(os.Stdin)
		if err != nil {
			clearRender()
			return nil, err
		}

		switch kp.Key {
		case KeyCtrlC, KeyCtrlD, KeyEscape:
			clearRender()
			return nil, ErrCanceled
		case KeyEnter:
			clearRender()
			if len(filtered) > 0 && cursor < len(filtered) {
				selected := filtered[cursor]
				return &selected, nil
			}
			return nil, ErrCanceled
		case KeyUp:
			if cursor > 0 {
				cursor--
			}
		case KeyDown:
			if cursor < len(filtered)-1 {
				cursor++
			}
		case KeyPageUp:
			cursor -= maxDisplay
			if cursor < 0 {
				cursor = 0
			}
		case KeyPageDown:
			cursor += maxDisplay
			if cursor >= len(filtered) {
				cursor = len(filtered) - 1
			}
		case KeyHome:
			cursor = 0
		case KeyEnd:
			if len(filtered) > 0 {
				cursor = len(filtered) - 1
			}
		case KeyBackspace, KeyDelete:
			if len(query) > 0 {
				query = query[:len(query)-1]
			}
		case KeyRune:
			if query == "" && (kp.Rune == 'k' || kp.Rune == 'j') {
				if kp.Rune == 'k' && cursor > 0 {
					cursor--
					continue
				} else if kp.Rune == 'j' && cursor < len(filtered)-1 {
					cursor++
					continue
				}
			}
			query += string(kp.Rune)
		}
	}
}
