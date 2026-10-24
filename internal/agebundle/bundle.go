package agebundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"filippo.io/age"
	"filippo.io/age/armor"
)

type payload struct {
	Secrets map[string]string `json:"secrets"`
}

// Encrypt writes an age-encrypted bundle of secrets to w.
func Encrypt(w io.Writer, secrets map[string][]byte, recipients []age.Recipient) error {
	if len(recipients) == 0 {
		return fmt.Errorf("no age recipients")
	}
	p := payload{Secrets: map[string]string{}}
	for k, v := range secrets {
		p.Secrets[k] = string(v)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	aw, err := age.Encrypt(&buf, recipients...)
	if err != nil {
		return err
	}
	if _, err := aw.Write(raw); err != nil {
		return err
	}
	if err := aw.Close(); err != nil {
		return err
	}
	armW := armor.NewWriter(w)
	if _, err := io.Copy(armW, &buf); err != nil {
		return err
	}
	return armW.Close()
}

// Decrypt reads an armored or binary age bundle from r.
func Decrypt(r io.Reader, identities ...age.Identity) (map[string][]byte, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var inner io.Reader = bytes.NewReader(raw)
	if bytes.HasPrefix(raw, []byte("-----BEGIN AGE ENCRYPTED FILE-----")) {
		ar := armor.NewReader(bytes.NewReader(raw))
		inner = ar
	}
	out, err := age.Decrypt(inner, identities...)
	if err != nil {
		return nil, err
	}
	payloadRaw, err := io.ReadAll(out)
	if err != nil {
		return nil, err
	}
	var p payload
	if err := json.Unmarshal(payloadRaw, &p); err != nil {
		return nil, err
	}
	result := map[string][]byte{}
	for k, v := range p.Secrets {
		result[k] = []byte(v)
	}
	return result, nil
}
