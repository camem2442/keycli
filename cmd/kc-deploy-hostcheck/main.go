// kc-deploy-hostcheck inspects ONLY names of environment variables.
// It does not read credential values, KeyCLI store, Keychain or Cloudflare.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/camem2442/keycli/internal/deploycustody"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] != "--json" {
		fmt.Fprintln(os.Stderr, "usage: kc-deploy-hostcheck --json")
		os.Exit(2)
	}
	report := deploycustody.ProbeEnvironment(os.LookupEnv)
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		os.Exit(2)
	}
}
