// Endpoint helpers shared by the network adapters.
//
// These are pure string/URL checks. They live here so the redirect guard exists once:
// two adapters comparing URLs slightly differently is how one of them ends up following
// a redirect the other refuses.
package oauth

import (
	"mime"
	"net/url"
	"strings"
)

// SecureEndpoint reports whether raw is an HTTPS URL usable as a protocol endpoint: a
// host, no credentials, no query or fragment, and not port 0.
func SecureEndpoint(raw string) bool { return secureEndpoint(raw) }

// SameEndpoint reports whether got is the URL want names.
//
// The comparison is exact for scheme, host (including port), and query, and compares the
// escaped path so an encoded separator stays distinct. Fragment is ignored because it is
// never sent. A nil got is never the same endpoint.
func SameEndpoint(want string, got *url.URL) bool {
	if got == nil {
		return false
	}
	parsed, err := url.Parse(want)
	if err != nil {
		return false
	}
	return sameURL(parsed, got)
}

func sameURL(a, b *url.URL) bool {
	if !strings.EqualFold(a.Scheme, b.Scheme) || !strings.EqualFold(a.Host, b.Host) {
		return false
	}
	if a.User != nil || b.User != nil {
		return false
	}
	if a.RawQuery != b.RawQuery {
		return false
	}
	return normalizePath(a) == normalizePath(b)
}

func normalizePath(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

// IsJSONMediaType reports whether contentType is application/json or the structured-suffix
// form RFC 6839 defines (for example application/oauth-metadata+json). Parameters such as
// charset are ignored.
func IsJSONMediaType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	if mediaType == "application/json" {
		return true
	}
	return strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json")
}
