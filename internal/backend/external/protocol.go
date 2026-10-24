package external

// Protocol version for kc ↔ kc-backend-* JSON on stdin/stdout.
const ProtocolVersion = 1

// Request is one JSON object per plugin invocation (stdin).
type Request struct {
	V    int    `json:"v"`
	Op   string `json:"op"`
	Name string `json:"name,omitempty"`

	ValueBase64 string  `json:"value_base64,omitempty"`
	Note        string  `json:"note,omitempty"`
	Expires     *string `json:"expires,omitempty"`

	Backend string `json:"backend,omitempty"` // handshake: config instance name
}

// Response is one JSON object on stdout (no secrets in stderr).
type Response struct {
	V   int    `json:"v"`
	OK  bool   `json:"ok"`
	Err string `json:"error,omitempty"`

	Protocol     int          `json:"protocol,omitempty"`
	Capabilities Capabilities `json:"capabilities,omitempty"`

	ValueBase64 string `json:"value_base64,omitempty"`
	Items       []Item `json:"items,omitempty"`

	// Errno is a stable machine code when ok is false.
	Errno string `json:"errno,omitempty"`
}

// Capabilities mirrors backend.Capabilities for the wire format.
type Capabilities struct {
	CanWrite     bool `json:"can_write"`
	CanList      bool `json:"can_list"`
	Interactive  bool `json:"interactive"`
	NeedsNetwork bool `json:"needs_network"`
}

// Item is list metadata without secret values.
type Item struct {
	Name    string  `json:"name"`
	Note    string  `json:"note,omitempty"`
	Expires *string `json:"expires,omitempty"`
}
