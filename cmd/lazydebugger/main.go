package main

import (
	"os"
	"os/exec"
)

func main() {
	// Run lazyjournal as a subprocess
	cmd := exec.Command("lazyjournal")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}
