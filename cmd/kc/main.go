package main

import (
	"os"

	"github.com/camem2442/keycli/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
