package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/epalmerini/rabbithole/internal/config"
	"github.com/epalmerini/rabbithole/internal/tui"
	"github.com/epalmerini/rabbithole/internal/xdg"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("rabbithole %s\n", version)
		return
	}

	configDir, err := xdg.Dir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving config directory: %v\n", err)
		os.Exit(1)
	}

	fileCfg, err := config.LoadFileConfig(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if err := tui.Run(fileCfg, configDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
