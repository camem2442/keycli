package keychain

import (
	"sort"
	"strconv"
	"strings"
)

// DumpEntry is one generic-password record from `security dump-keychain`.
type DumpEntry struct {
	Service string
	Account string
}

// ParseDumpEntries parses the attribute blocks printed by
// `security dump-keychain`. Real macOS output uses:
//
//	class: 0x00000010
//	attributes:
//	    0x00000007 <blob>="service"
//	    "acct"<blob>="account"
//	    "svce"<blob>="service"
//
// Older fakes and some tools print `service: name`; both are accepted.
func ParseDumpEntries(dump string) []DumpEntry {
	var out []DumpEntry
	var cur *DumpEntry
	flush := func() {
		if cur != nil && cur.Service != "" {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(dump, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "class:") {
			flush()
			cur = &DumpEntry{}
			continue
		}
		if strings.HasPrefix(line, "service:") {
			flush()
			svc := strings.TrimSpace(strings.TrimPrefix(line, "service:"))
			if svc != "" {
				out = append(out, DumpEntry{Service: svc})
			}
			continue
		}
		name, val, ok := parseDumpAttr(line)
		if !ok {
			continue
		}
		if cur == nil {
			cur = &DumpEntry{}
		}
		switch name {
		case "svce":
			cur.Service = val
		case "acct":
			cur.Account = val
		}
	}
	flush()
	return out
}

// parseDumpAttr parses `"svce"<blob>="value"` (quoted, <NULL>, or bare).
func parseDumpAttr(line string) (string, string, bool) {
	if !strings.HasPrefix(line, `"`) {
		return "", "", false
	}
	end := strings.Index(line[1:], `"`)
	if end < 0 {
		return "", "", false
	}
	name := line[1 : 1+end]
	rest := strings.TrimSpace(line[2+end:])
	const marker = "<blob>="
	if !strings.HasPrefix(rest, marker) {
		return "", "", false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(rest, marker))
	if raw == "<NULL>" || raw == "" {
		return name, "", true
	}
	if strings.HasPrefix(raw, `"`) {
		if v, err := strconv.Unquote(raw); err == nil {
			return name, v, true
		}
		// Tolerate trailing annotations such as a commented hex dump.
		if idx := strings.LastIndex(raw, `"`); idx > 1 {
			if v, err := strconv.Unquote(raw[:idx+1]); err == nil {
				return name, v, true
			}
		}
		return name, strings.Trim(raw, `"`), true
	}
	return name, raw, true
}

// sortedUnique returns a stable, de-duplicated list.
func sortedUnique(seen map[string]struct{}) []string {
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
