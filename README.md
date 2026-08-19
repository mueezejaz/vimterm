# vimterm

A Vim-like terminal emulator for Windows. It launches a shell (PowerShell, cmd.exe, WSL, ...) inside a ConPTY, renders its output, and lets you drive the terminal with modal, Vim-style keybindings: move around the screen and scrollback, search, yank and paste, record macros — while the shell keeps running underneath.

## Requirements

- Windows 10 1809 or later (ConPTY)
- If building from source: Go 1.26+

## Install

Grab the latest `vimterm.exe` (or the zip) from the [Releases](https://github.com/mueezejaz/vimterm/releases) page and run it. Your config file is created automatically on first launch.

## Build from source

```
go build -o vimterm.exe .
```

## Usage

```
vimterm [options]
  -config path   path to config file (default: %APPDATA%\vimterm\config.toml)
  -shell prog    shell program to launch (overrides config)
```

Quit with `ctrl+q` or `:quit`.

## Configuration

The config lives at `%APPDATA%\vimterm\config.toml` and is created with all defaults and comments on first launch. Editing it reloads bindings live (checked once per second); other settings apply on the next launch.

Example:

```toml
[general]
shell = "pwsh.exe"
shell_args = ["-NoLogo"]
scrollback = 20000
leader = "space"
timeoutlen = 1000

[colors]
status_fg = "#00ff87"
status_bg = "#262335"

[commands]
clean = "leader+c"

[keybindings.normal]
"leader+c" = "scroll_up"
"ctrl+y" = "scroll_down"
```

### `[general]`

| key         | default           | description                                              |
|-------------|-------------------|----------------------------------------------------------|
| `shell`     | `"powershell.exe"`| Program launched inside the terminal                     |
| `shell_args`| `[]`              | Extra arguments passed to the shell                      |
| `scrollback`| `10000`           | Max scrolled-off lines kept in memory                    |
| `leader`    | `"space"`         | Leader key, used in bindings as the `leader` token       |
| `timeoutlen`| `1000`            | ms a partial key sequence (e.g. the first `g` of `gg`) waits for completion |
| `status_merge`| `"auto"`      | Full-screen apps (nvim) get the full height; vimterm's status bar overlays their status line while a message shows. `"auto"` merges only when the bottom row looks like a status line, `"always"` unconditionally, `"never"` keeps the vimterm bar below the app |

### `[colors]`

Status line colors, `#rrggbb`; empty string = terminal default.

- `status_fg`
- `status_bg`

### `[commands]`

Custom colon-commands: a name maps to a key sequence (binding token syntax) that is replayed through the keybinding engine.

```toml
[commands]
clean = "leader+c"   # now :clean runs leader+c
```

Built-in colon-commands (cannot be overridden): `:quit` (exit), `:clear` (clear scrollback), `:shell` (restart the shell).

### `[keybindings.normal]`, `[keybindings.insert]`, `[keybindings.visual]`

Each section maps a key sequence token to an action name. Omit a section to keep the defaults; define one to fully replace it.

Token syntax:

- Single keys: `h`, `G`, `f5`
- Sequences: `gg` (two keys typed in order)
- Modifiers: `ctrl+u`, `shift+tab`, `alt+x`, `ctrl+f5`
- Named keys: `esc`, `enter`, `backspace`, `tab`, `up`, `down`, `left`, `right`, `home`, `end`, `pageup`, `pagedown`, `insert`, `delete`
- `leader+t` — the leader key, then `t`
- Uppercase/punctuation like `:` implies shift

Available actions:

| action | action |
|---|---|
| `move_left` `move_down` `move_up` `move_right` | `goto_top` `goto_bottom` |
| `scroll_up` `scroll_down` | `enter_insert` `enter_insert_after` `enter_insert_end` `enter_insert_home` `enter_normal` |
| `search_forward` `search_next` `search_prev` | `command_prompt` |
| `enter_visual` `enter_visual_line` `cancel_visual` | `yank` `paste` `paste_before` |
| `yank_line` `delete_word` `delete_word_back` | `record_macro` `play_macro` `repeat_last` |
| `find_char` `find_char_back` `find_until` `find_until_back` `find_next` `find_prev` | `move_word` `move_word_back` `move_word_end` `move_word_upper` `move_word_back_upper` `move_word_end_upper` |
| `quit` | |

### Default bindings

Normal mode mirrors Vim: `h/j/k/l` and arrows move, `gg`/`G` jump, `ctrl+u`/`ctrl+d` scroll, `i`/`a`/`A`/`I` enter insert, `/` searches (`n`/`N` next/prev), `:` opens the command prompt, `v`/`V` visual, `y`/`p`/`P` yank/paste, `yy` yanks the whole line (with a count, several lines), `dw`/`db` delete (and cut) the word forward/backward, `q`/`@` record/play macro, `.` repeats the last command, `f`/`F`/`t`/`T`/`;`/`,` find, `w`/`b`/`e` and `W`/`B`/`E` word motions, `ctrl+q` quits. Insert mode binds only `esc` and `ctrl+q`; everything else types through to the shell.
