package interactive

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/clip"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/resolve"
	"golang.org/x/term"
)

// ClipboardCopyFunc is a function signature for copying secrets with auto-clear.
type ClipboardCopyFunc func(a *app.App, text string, osc52 bool) error

// RunManager launches the full interactive secrets manager TUI.
func RunManager(ctx context.Context, a *app.App, copyFunc ClipboardCopyFunc) error {
	if !IsTerminal(os.Stdin) {
		return fmt.Errorf("대화형 모드는 터미널에서만 지원됩니다")
	}

	fd := int(os.Stdin.Fd())

	// Fetch initial secrets
	items, err := loadItems(ctx, a)
	if err != nil {
		return err
	}

	query := ""
	cursor := 0
	statusMsg := ""
	statusIsError := false

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

		// Draw Screen in raw mode
		oldState, err := term.MakeRaw(fd)
		if err != nil {
			return err
		}
		fmt.Fprint(os.Stderr, HideCursor)

		// Get terminal width/height if possible
		width, height, err := term.GetSize(fd)
		if err != nil || width <= 0 {
			width = 80
		}
		if height <= 0 {
			height = 24
		}

		maxDisplay := height - 12
		if maxDisplay < 4 {
			maxDisplay = 4
		}

		start := 0
		if cursor >= maxDisplay {
			start = cursor - maxDisplay + 1
		}
		end := start + maxDisplay
		if end > len(filtered) {
			end = len(filtered)
		}

		var b strings.Builder
		// Clear screen and move to top
		b.WriteString("\033[H\033[2J")

		// Header
		b.WriteString(fmt.Sprintf("%s%s🔐 keycli (kc) — 비밀키 및 시크릿 관리자%s\r\n", Bold, Cyan, Reset))
		b.WriteString(fmt.Sprintf("%s────────────────────────────────────────────────────────────%s\r\n", Gray, Reset))
		b.WriteString(fmt.Sprintf("%s검색어:%s %s%s%s%s (등록된 시크릿: %d개 / 검색결과: %d개)\r\n\r\n",
			Bold, Reset, Yellow, query, Reset, Dim, len(items), len(filtered)))

		// List of items
		if len(filtered) == 0 {
			b.WriteString(fmt.Sprintf("  %s일치하는 시크릿이 없습니다. [a] 키를 눌러 새로 추가할 수 있습니다.%s\r\n", Gray, Reset))
		} else {
			for i := start; i < end; i++ {
				it := filtered[i]
				isSelected := (i == cursor)
				prefix := "   "
				nameColor := Reset

				if isSelected {
					prefix = fmt.Sprintf("%s ❯%s ", Cyan, Reset)
					nameColor = fmt.Sprintf("%s%s", Bold, Cyan)
				}

				nameCol := PadRight(it.Name, 24)
				backendCol := ""
				if it.Backend != "" {
					backendCol = fmt.Sprintf("%s[%s]%s", Gray, it.Backend, Reset)
				}
				backendCol = PadRight(backendCol, 12)

				noteCol := ""
				if it.Note != "" {
					noteCol = fmt.Sprintf("%s%s%s", Dim, TruncateString(it.Note, 28), Reset)
				}

				badge := ""
				if it.PolicyDenied {
					badge = fmt.Sprintf(" %s[policy-denied]%s", Red, Reset)
				} else if it.StaleExport {
					badge = fmt.Sprintf(" %s[stale *]%s", Yellow, Reset)
				}

				b.WriteString(fmt.Sprintf("%s%s%s%s %s %s%s\r\n", prefix, nameColor, nameCol, Reset, backendCol, noteCol, badge))
			}
		}

		b.WriteString("\r\n")

		// Selected Item Details Box
		b.WriteString(fmt.Sprintf("%s── 상세 정보 ──────────────────────────────────────────────%s\r\n", Gray, Reset))
		if len(filtered) > 0 && cursor < len(filtered) {
			it := filtered[cursor]
			b.WriteString(fmt.Sprintf("  %s이름:%s %s%s%s", Bold, Reset, Cyan, it.Name, Reset))
			if len(it.Aliases) > 0 {
				b.WriteString(fmt.Sprintf("  %s(별칭: %s)%s", Gray, strings.Join(it.Aliases, ", "), Reset))
			}
			b.WriteString("\r\n")

			backendInfo := it.Backend
			if it.Source != "" {
				backendInfo += fmt.Sprintf(" (%s)", it.Source)
			}
			b.WriteString(fmt.Sprintf("  %s저장소:%s %s\r\n", Bold, Reset, backendInfo))

			if it.Note != "" {
				b.WriteString(fmt.Sprintf("  %s메모:%s %s\r\n", Bold, Reset, it.Note))
			}
			if it.Expires != nil {
				b.WriteString(fmt.Sprintf("  %s만료일:%s %s\r\n", Bold, Reset, *it.Expires))
			}
		} else {
			b.WriteString(fmt.Sprintf("  %s선택된 시크릿이 없습니다.%s\r\n", Gray, Reset))
		}

		b.WriteString(fmt.Sprintf("%s────────────────────────────────────────────────────────────%s\r\n", Gray, Reset))

		// Status / Notification Line
		if statusMsg != "" {
			color := Green
			if statusIsError {
				color = Red
			}
			b.WriteString(fmt.Sprintf("%s%s%s\r\n", color, statusMsg, Reset))
		} else {
			b.WriteString("\r\n")
		}

		// Help / Keybind bar
		b.WriteString(fmt.Sprintf("%s[Enter/c] 복사  [v] 값 보기  [a] 추가  [d] 삭제  [r] 새로고침  [q] 종료%s\r\n", Inverse, Reset))

		fmt.Fprint(os.Stderr, b.String())

		// Read key
		kp, err := ReadKey(os.Stdin)

		// Restore terminal before performing any sub-action
		term.Restore(fd, oldState)
		fmt.Fprint(os.Stderr, ShowCursor)

		if err != nil {
			return err
		}

		// Reset status message on next keypress unless newly set
		prevStatus := statusMsg
		statusMsg = ""
		statusIsError = false

		switch kp.Key {
		case KeyCtrlC, KeyCtrlD:
			fmt.Fprintln(os.Stderr)
			return nil
		case KeyEscape:
			if query != "" {
				query = ""
				continue
			}
			fmt.Fprintln(os.Stderr)
			return nil
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
		case KeyEnter:
			// Copy selected secret
			if len(filtered) > 0 && cursor < len(filtered) {
				it := filtered[cursor]
				statusMsg, statusIsError = copySecret(ctx, a, it.Name, copyFunc)
			}
		case KeyRune:
			r := kp.Rune
			switch r {
			case 'q', 'Q':
				if query == "" {
					fmt.Fprintln(os.Stderr)
					return nil
				}
				query += string(r)
			case 'c', 'C':
				if query == "" && len(filtered) > 0 && cursor < len(filtered) {
					it := filtered[cursor]
					statusMsg, statusIsError = copySecret(ctx, a, it.Name, copyFunc)
				} else {
					query += string(r)
				}
			case 'v', 'V':
				if query == "" && len(filtered) > 0 && cursor < len(filtered) {
					it := filtered[cursor]
					viewSecretDetails(ctx, a, it.Name)
					statusMsg = prevStatus
				} else {
					query += string(r)
				}
			case 'a', 'A':
				if query == "" {
					newName, err := RunSetWizard(ctx, a, "")
					if err == nil && newName != "" {
						statusMsg = fmt.Sprintf("✓ 시크릿 '%s'이(가) 추가되었습니다.", newName)
						items, _ = loadItems(ctx, a)
					} else if err != nil && err != ErrCanceled {
						statusMsg = fmt.Sprintf("오류: %v", err)
						statusIsError = true
					}
				} else {
					query += string(r)
				}
			case 'd', 'D':
				if query == "" && len(filtered) > 0 && cursor < len(filtered) {
					it := filtered[cursor]
					ok, err := PromptConfirm(fmt.Sprintf("정말로 시크릿 '%s'을(를) 삭제하시겠습니까?", it.Name), false)
					if err == nil && ok {
						if err := a.Resolver.Delete(ctx, it.Name); err != nil {
							statusMsg = fmt.Sprintf("삭제 실패: %v", err)
							statusIsError = true
						} else {
							statusMsg = fmt.Sprintf("✓ 시크릿 '%s'이(가) 삭제되었습니다.", it.Name)
							items, _ = loadItems(ctx, a)
						}
					}
				} else {
					query += string(r)
				}
			case 'r', 'R':
				if query == "" {
					items, err = loadItems(ctx, a)
					if err != nil {
						statusMsg = fmt.Sprintf("새로고침 실패: %v", err)
						statusIsError = true
					} else {
						statusMsg = "목록을 새로고침했습니다."
					}
				} else {
					query += string(r)
				}
			case 'j':
				if query == "" && cursor < len(filtered)-1 {
					cursor++
				} else {
					query += string(r)
				}
			case 'k':
				if query == "" && cursor > 0 {
					cursor--
				} else {
					query += string(r)
				}
			default:
				query += string(r)
			}
		}
	}
}

func loadItems(ctx context.Context, a *app.App) ([]resolve.ListItem, error) {
	items, err := a.Resolver.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func copySecret(ctx context.Context, a *app.App, name string, copyFunc ClipboardCopyFunc) (string, bool) {
	val, _, err := a.Resolver.Get(ctx, name)
	if err != nil {
		if kcerrors.Code(err) == kcerrors.ExitDenied {
			return fmt.Sprintf("kc: 정책에 의해 '%s' 읽기가 거부되었습니다.", name), true
		}
		return fmt.Sprintf("복사 실패: %v", err), true
	}
	text := string(val)
	if copyFunc != nil {
		if err := copyFunc(a, text, os.Getenv("KC_CLIP_OSC52") == "1"); err != nil {
			return fmt.Sprintf("클립보드 복사 실패: %v", err), true
		}
	} else {
		cp := clip.Provider{Config: a.Config}
		if err := cp.Write(text); err != nil {
			return fmt.Sprintf("클립보드 복사 실패: %v", err), true
		}
	}
	return fmt.Sprintf("✓ '%s' 클립보드에 복사 완료 (%v 후 자동 삭제)", name, clip.AutoClearDelay()), false
}

func viewSecretDetails(ctx context.Context, a *app.App, name string) {
	val, backendName, src, err := a.Resolver.GetWithSource(ctx, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n%s오류: %v%s\n", Red, err, Reset)
		PromptLine("계속하려면 Enter를 누르세요", "")
		return
	}

	fmt.Fprintf(os.Stderr, "\n%s%s=== 시크릿 상세 정보: %s ===%s\n", Bold, Cyan, name, Reset)
	fmt.Fprintf(os.Stderr, "  저장소: %s\n", backendName)
	if src != "" {
		fmt.Fprintf(os.Stderr, "  출처:   %s\n", src)
	}
	fmt.Fprintf(os.Stderr, "  크기:   %d 바이트\n", len(val))

	reveal, _ := PromptConfirm("시크릿 값을 화면에 표시하시겠습니까?", false)
	if reveal {
		fmt.Fprintf(os.Stderr, "  %s값:%s    %s\n", Bold, Reset, string(val))
	} else {
		masked := strings.Repeat("*", len(val))
		if len(val) > 20 {
			masked = strings.Repeat("*", 20) + "..."
		}
		fmt.Fprintf(os.Stderr, "  %s값:%s    %s %s(마스킹됨)%s\n", Bold, Reset, masked, Gray, Reset)
	}
	fmt.Fprintln(os.Stderr)
	PromptLine("메뉴로 돌아가려면 Enter를 누르세요", "")
}
