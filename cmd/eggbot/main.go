// Command eggbot is the IRC bot binary. Usage: eggbot -c eggbot.toml
package main

import (
	"os"

	"eggbot/internal/app"
)

func main() {
	os.Exit(app.Main(os.Args[1:]))
}
