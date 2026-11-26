package totp

import (
	"strings"
	"testing"
	"time"
)

func base32Of(s string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	var out strings.Builder
	var buf uint32
	bits := 0
	for i := 0; i < len(s); i++ {
		buf = buf<<8 | uint32(s[i])
		bits += 8
		for bits >= 5 {
			out.WriteByte(alphabet[(buf>>uint(bits-5))&0x1f])
			bits -= 5
		}
	}
	if bits > 0 {
		out.WriteByte(alphabet[(buf<<uint(5-bits))&0x1f])
	}
	return out.String()
}

// RFC 6238 appendix B vectors.
func TestRFC6238Vectors(t *testing.T) {
	seeds := map[string]string{
		"SHA1":   "12345678901234567890",
		"SHA256": "12345678901234567890123456789012",
		"SHA512": "1234567890123456789012345678901234567890123456789012345678901234",
	}
	vectors := []struct {
		unix   int64
		sha1   string
		sha256 string
		sha512 string
	}{
		{59, "94287082", "46119246", "90693936"},
		{1111111109, "07081804", "68084774", "25091201"},
		{20000000000, "65353130", "77737706", "47863826"},
	}
	for _, v := range vectors {
		for alg, want := range map[string]string{"SHA1": v.sha1, "SHA256": v.sha256, "SHA512": v.sha512} {
			uri := "otpauth://totp/Demo:me?secret=" + base32Of(seeds[alg]) +
				"&algorithm=" + alg + "&digits=8&period=30"
			cfg, err := Parse(uri)
			if err != nil {
				t.Fatalf("%s parse: %v", alg, err)
			}
			got, err := cfg.Code(time.Unix(v.unix, 0))
			if err != nil {
				t.Fatalf("%s code: %v", alg, err)
			}
			if got != want {
				t.Fatalf("%s at %d = %s, want %s", alg, v.unix, got, want)
			}
		}
	}
}

func TestParseAppleLabelLessURI(t *testing.T) {
	uri := "otpauth://totp?secret=" + base32Of("12345678901234567890") +
		"&algorithm=SHA1&digits=6&period=30"
	cfg, err := Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Digits() != 6 || cfg.Period() != 30 || cfg.Algorithm() != "SHA1" {
		t.Fatalf("params: %d %d %s", cfg.Digits(), cfg.Period(), cfg.Algorithm())
	}
	code, err := cfg.Code(time.Unix(59, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Fatalf("code length %d", len(code))
	}
}

func TestParseBareSeedDefaults(t *testing.T) {
	cfg, err := Parse(strings.ToLower(base32Of("12345678901234567890")))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Digits() != 6 || cfg.Period() != 30 || cfg.Algorithm() != "SHA1" {
		t.Fatal("defaults not applied")
	}
}

func TestParseRejectsHOTP(t *testing.T) {
	if _, err := Parse("otpauth://hotp/Demo?secret=" + base32Of("1234") + "&counter=1"); err == nil {
		t.Fatal("HOTP accepted")
	}
}

func TestParseErrorsDoNotRepeatTheSeed(t *testing.T) {
	seed := base32Of("super-secret-seed-material")
	bad := []string{
		"otpauth://totp/demo?secret=" + seed + "&algorithm=MD5",
		"otpauth://totp/demo?secret=" + seed + "&digits=12",
		"otpauth://totp/demo?secret=!!!notbase32!!!",
	}
	for _, uri := range bad {
		_, err := Parse(uri)
		if err == nil {
			t.Fatalf("accepted %q", uri)
		}
		if strings.Contains(err.Error(), seed) || strings.Contains(err.Error(), "notbase32") {
			t.Fatalf("error repeats the secret: %v", err)
		}
	}
}

func TestZeroConfigIsRejected(t *testing.T) {
	if _, err := (Config{}).Code(time.Unix(0, 0)); err == nil {
		t.Fatal("zero config produced a code")
	}
}

func TestConfigStringIsRedacted(t *testing.T) {
	cfg, err := Parse(base32Of("12345678901234567890"))
	if err != nil {
		t.Fatal(err)
	}
	if s := cfg.String(); s != "totp.Config{redacted}" {
		t.Fatalf("unexpected String: %q", s)
	}
	if s := cfg.GoString(); s != "totp.Config{redacted}" {
		t.Fatalf("unexpected GoString: %q", s)
	}
}

func TestStepRollover(t *testing.T) {
	cfg, err := Parse(base32Of("12345678901234567890"))
	if err != nil {
		t.Fatal(err)
	}
	first, _ := cfg.Code(time.Unix(59, 0))
	second, _ := cfg.Code(time.Unix(60, 0))
	if first == second {
		t.Fatal("code did not roll over at the step boundary")
	}
	if first, _ := cfg.Code(time.Unix(30, 0)); first == "" {
		t.Fatal("empty code")
	}
}
