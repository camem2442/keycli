package interactive

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/clip"
)

// RunSetWizard guides the user through setting a new or updated secret.
func RunSetWizard(ctx context.Context, a *app.App, initialName string) (string, error) {
	fmt.Fprintf(os.Stderr, "\n%s%s🔑 keycli 시크릿 저장 마법사%s\n", Bold, Cyan, Reset)
	fmt.Fprintf(os.Stderr, "%s────────────────────────────%s\n", Gray, Reset)

	name := strings.TrimSpace(initialName)
	for name == "" {
		val, err := PromptLine("시크릿 이름 (예: OPENAI_API_KEY, clerk_secret_key)", "")
		if err != nil {
			return "", err
		}
		name = strings.TrimSpace(val)
		if name == "" {
			fmt.Fprintf(os.Stderr, "%s이름을 입력해주세요.%s\n", Yellow, Reset)
		}
	}

	// Check clipboard availability
	cp := clip.Provider{Config: a.Config}
	clipText, clipErr := cp.Read()
	hasClipboard := clipErr == nil && len(strings.TrimSpace(clipText)) > 0

	var options []string
	if hasClipboard {
		options = append(options, fmt.Sprintf("클립보드에서 붙여넣기 (%d 글자)", len(strings.TrimSpace(clipText))))
	} else {
		options = append(options, "클립보드에서 붙여넣기 (비어있음)")
	}
	options = append(options, "보안 입력 (화면에 노출되지 않음)")
	options = append(options, "일반 텍스트 입력")

	choice, err := PromptChoice("시크릿 값을 입력할 방식을 선택하세요:", options, 0)
	if err != nil {
		return "", err
	}

	var secretValue []byte
	fromClip := false

	switch choice {
	case 0: // Clipboard
		if !hasClipboard {
			// re-read or prompt
			t, err := cp.Read()
			if err != nil || len(strings.TrimSpace(t)) == 0 {
				return "", fmt.Errorf("클립보드에 읽을 수 있는 내용이 없습니다")
			}
			clipText = t
		}
		secretValue = []byte(strings.TrimRight(clipText, "\r\n"))
		fromClip = true
		fmt.Fprintf(os.Stderr, "%s✓ 클립보드에서 %d 바이트를 읽었습니다.%s\n", Green, len(secretValue), Reset)

	case 1: // Hidden password
		val, err := PromptPassword("비밀 값 입력")
		if err != nil {
			return "", err
		}
		if len(val) == 0 {
			return "", fmt.Errorf("시크릿 값이 비어 있습니다")
		}
		secretValue = val

	case 2: // Plain text
		val, err := PromptLine("시크릿 값 입력", "")
		if err != nil {
			return "", err
		}
		if len(val) == 0 {
			return "", fmt.Errorf("시크릿 값이 비어 있습니다")
		}
		secretValue = []byte(val)
	}

	// Optional Note
	note, err := PromptLine("설명/메모 (선택사항, Enter로 건너뛰기)", "")
	if err != nil {
		return "", err
	}

	// Optional Expiry
	var exp *time.Time
	for {
		expStr, err := PromptLine("만료일 (YYYY-MM-DD, 선택사항, Enter로 건너뛰기)", "")
		if err != nil {
			return "", err
		}
		expStr = strings.TrimSpace(expStr)
		if expStr == "" {
			break
		}
		t, err := time.Parse("2006-01-02", expStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s올바른 날짜 형식(YYYY-MM-DD)을 입력하세요.%s\n", Yellow, Reset)
			continue
		}
		exp = &t
		break
	}

	// Save to resolver
	if err := a.Resolver.Set(ctx, name, secretValue, backend.Meta{
		Note:    note,
		Expires: exp,
	}); err != nil {
		return "", err
	}

	// Clear clipboard if fromClip
	if fromClip {
		_ = cp.Clear()
	}

	fmt.Fprintf(os.Stderr, "%s✓ 시크릿 '%s'이(가) 안전하게 저장되었습니다.%s\n\n", Green, name, Reset)
	return name, nil
}
