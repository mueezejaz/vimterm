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
	"path/filepath"
	"reflect"
	"strings"

	"vimterm/internal/app"
	"vimterm/internal/config"
)

func main() {
	configPath := flag.String("config", "", "path to config file (default: %APPDATA%\\vimterm\\config.toml)")
	shell := flag.String("shell", "", "shell program to launch (overrides config)")
	dirFlag := flag.String("dir", "", "working directory for the shell (overrides config)")

	// A bare positional argument is a working directory. Launchers that
	// integrate a file manager ("open this folder in a terminal") pass the
	// path as an argument rather than behind a flag, and ignoring it made
	// vimterm open in the wrong place with no sign of why.
	//
	// flag.Parse stops at the first non-flag argument and treats the rest as
	// positional, so `vimterm.exe "C:\dir" -config x` would leave -config
	// unparsed. Positional arguments are therefore lifted out first and flags
	// parse normally wherever they appear.
	argv, positionals := splitArgs(os.Args[1:])
	if err := flag.CommandLine.Parse(argv); err != nil {
		os.Exit(2)
	}

	switch len(positionals) {
	case 0:
	case 1:
		if *dirFlag == "" {
			*dirFlag = positionals[0]
		}
	default:
		fmt.Fprintf(os.Stderr, "vimterm: unexpected extra arguments: %q\n", positionals[1:])
		fmt.Fprintln(os.Stderr, "usage: vimterm [-config path] [-shell prog] [-dir path] [dir]")
		os.Exit(2)
	}
	dir := *dirFlag

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
	// Validate the directory before the terminal takes over the screen: a bad
	// one here would otherwise be reported by the shell after vimterm has
	// already cleared and repainted, and reads as a rendering glitch.
	abs, err := resolveDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vimterm: %v\n", err)
		os.Exit(1)
	}
	if abs != "" {
		cfg.General.Dir = abs
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := app.Run(ctx, cfg, path); err != nil {
		fmt.Fprintf(os.Stderr, "vimterm: %v\n", err)
		os.Exit(1)
	}
}

// resolveDir turns a directory argument into an absolute path, rejecting a
// path that does not exist or is not a directory. An empty argument means
// "inherit" and returns empty with no error.
func resolveDir(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("-dir %s: %w", dir, err)
	}
	info, err := os.Stat(abs)
	switch {
	case os.IsNotExist(err):
		return "", fmt.Errorf("-dir %s: no such directory", abs)
	case err != nil:
		return "", fmt.Errorf("-dir %s: %w", abs, err)
	case !info.IsDir():
		return "", fmt.Errorf("-dir %s: not a directory", abs)
	}
	return abs, nil
}

// splitArgs separates positional arguments from flags so a directory may be
// given before, between or after flags. Everything after "--" is positional,
// matching the usual convention.
//
// A flag written as "-x value" consumes the following argument as its value;
// without that step the value would be mistaken for a positional directory.
func splitArgs(args []string) (flags, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return args[:i], append(positional, args[i+1:]...)
		}
		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && flagNeedsValue(a) && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, a)
	}
	return flags, positional
}

// flagNeedsValue reports whether a flag takes a separate value argument. A
// boolean flag is satisfied by its presence alone and must not consume the next
// argument; vimterm's flags are all strings, and an unrecognized flag is left
// for flag.Parse to reject rather than guessed at here.
func flagNeedsValue(name string) bool {
	f := flag.CommandLine.Lookup(strings.TrimLeft(name, "-"))
	if f == nil {
		return false
	}
	return reflect.TypeOf(f.Value).String() == "*flag.stringValue"
}
