// Command cct is an unofficial CLI for local Codex session portability:
// export sessions on one device, move the bundle manually, import on another.
// No hosting, accounts, cloud sync, or SQLite modification.
package main

import (
	"os"

	"github.com/ahmojo/codex-claude-transfer/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
