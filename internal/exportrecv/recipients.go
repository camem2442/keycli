package exportrecv

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"filippo.io/age"
	"filippo.io/age/agessh"
)

// ParseRecipient parses age1… or ssh-… recipient strings.
func ParseRecipient(s string) (age.Recipient, bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false, fmt.Errorf("empty recipient")
	}
	if strings.HasPrefix(s, "age1") {
		r, err := age.ParseX25519Recipient(s)
		return r, false, err
	}
	if strings.HasPrefix(s, "ssh-") {
		r, err := agessh.ParseRecipient(s)
		return r, true, err
	}
	return nil, false, fmt.Errorf("recipient must be age1… or ssh-…")
}

// FetchGitHubKeys loads ssh public keys from https://github.com/USER.keys
func FetchGitHubKeys(user string, client *http.Client) ([]age.Recipient, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return nil, fmt.Errorf("empty github user")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	url := "https://github.com/" + user + ".keys"
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github keys: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var recipients []age.Recipient
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r, err := agessh.ParseRecipient(line)
		if err != nil {
			continue
		}
		recipients = append(recipients, r)
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("no ssh public keys found for %s", user)
	}
	return recipients, nil
}
