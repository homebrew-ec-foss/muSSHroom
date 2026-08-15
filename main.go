package main

import (
	"os"

	server "github.com/homebrew-ec-foss/muSSHroom/internal/server"
)

func main() {
	os.Setenv("FORCE_COLOR", "1")
	server.Start()
}
