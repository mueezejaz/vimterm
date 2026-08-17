// Command vimterm is a Vim-like terminal emulator for Windows.
//
// The shell runs inside a ConPTY; vimterm renders its output and captures
// input, providing modal (Vim-style) navigation and control.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"vimterm/internal/app"
	"vimterm/internal/config"
)

func main() {
	configPath := flag.String("config", "", "path to config file (default: %APPDATA%\\vimterm\\config.toml)")
	shell := flag.String("shell", "", "shell program to launch (overrides config)")
	flag.Parse()

	path := *configPath
	if path == "" {
		path = config.DefaultPath()
	}
	if err := config.EnsureDefault(path); err != nil {
		fmt.Fprintf(os.Stderr, "vimterm: %v\n", err)
		os.Exit(1)
	}
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vimterm: %v\n", err)
		os.Exit(1)
	}
	if *shell != "" {
		cfg.General.Shell = *shell
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := app.Run(ctx, cfg, path); err != nil {
		fmt.Fprintf(os.Stderr, "vimterm: %v\n", err)
		os.Exit(1)
	}
}
