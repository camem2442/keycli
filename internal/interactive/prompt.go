package interactive

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// PromptLine prompts the user for a text string with an optional default value.
func PromptLine(label string, defaultVal string) (string, error) {
	promptText := label
	if defaultVal != "" {
		promptText += fmt.Sprintf(" %s[%s]%s", Gray, defaultVal, Reset)
	}
	promptText += ": "

	fmt.Fprint(os.Stderr, promptText)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	res := strings.TrimSpace(line)
	if res == "" && defaultVal != "" {
		return defaultVal, nil
	}
	return res, nil
}

// PromptPassword prompts for a hidden secret string.
func PromptPassword(label string) ([]byte, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// PromptConfirm prompts the user with a yes/no question.
func PromptConfirm(question string, defaultYes bool) (bool, error) {
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}
	fmt.Fprintf(os.Stderr, "%s [%s]: ", question, hint)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans == "" {
		return defaultYes, nil
	}
	if ans == "y" || ans == "yes" {
		return true, nil
	}
	return false, nil
}

// PromptChoice presents a list of numbered choices to the user.
func PromptChoice(title string, options []string, defaultIdx int) (int, error) {
	fmt.Fprintf(os.Stderr, "%s%s%s\n", Bold, title, Reset)
	for i, opt := range options {
		prefix := " "
		if i == defaultIdx {
			prefix = "*"
		}
		fmt.Fprintf(os.Stderr, " %s%s%d)%s %s\n", prefix, Cyan, i+1, Reset, opt)
	}
	defaultPrompt := fmt.Sprintf("%d", defaultIdx+1)
	for {
		ans, err := PromptLine("선택 (번호)", defaultPrompt)
		if err != nil {
			return 0, err
		}
		var idx int
		n, _ := fmt.Sscanf(ans, "%d", &idx)
		if n == 1 && idx >= 1 && idx <= len(options) {
			return idx - 1, nil
		}
		fmt.Fprintf(os.Stderr, "%s올바른 번호를 입력하세요 (1-%d)%s\n", Yellow, len(options), Reset)
	}
}
