// Command dumpbytes spawns a shell the same way vimterm does and dumps the
// RAW bytes received from the ConPTY session to a hex+ASCII log, completely
// bypassing vimterm's emulator/render pipeline. It answers the question:
// are the bytes ConPTY delivers already corrupted, or does vimterm corrupt
// them later?
//
//	Usage:
//	  dumpbytes [-wrap] [-shell pwsh.exe] [-secs 6] [-out dump.log]
//
//	-wrap    true:  spawn via pty.Spawn (includes the chcp 65001 init wrapper)
//	         false: spawn pwsh.exe raw, bypassing the wrapper
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/xpty"
	"vimterm/internal/pty"
)

func main() {
	wrap := flag.Bool("wrap", true, "use pty.Spawn (chcp 65001 wrapper); false = raw spawn")
	shell := flag.String("shell", "pwsh.exe", "program to spawn")
	secs := flag.Int("secs", 6, "capture duration in seconds")
	out := flag.String("out", "dump.log", "output log path")
	bin := flag.String("bin", "", "optional path to also write the raw byte stream")
	flag.Parse()

	log, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "log:", err)
		os.Exit(1)
	}
	defer log.Close()
	w := bufio.NewWriter(log)
	defer w.Flush()

	var binw *bufio.Writer
	var binf *os.File
	if *bin != "" {
		binf, err = os.Create(*bin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "bin:", err)
			os.Exit(1)
		}
		defer binf.Close()
		binw = bufio.NewWriter(binf)
		defer binw.Flush()
	}

	// Query commands sent to the child once it is up, so we can see the
	// codepage/encoding it believes it is using. Only valid for shells.
	probe := "Write-Host 'VTDIAG_BEGIN'; chcp; " +
		"Write-Host (\"OutEnc=\" + [Console]::OutputEncoding.WebName); " +
		"Write-Host (\"InEnc=\" + [Console]::InputEncoding.WebName); " +
		"Write-Host 'VTDIAG_END';\r\n"

	var sess io.ReadWriteCloser
	if *wrap {
		s, err := pty.Spawn(*shell, nil, 120, 30)
		if err != nil {
			fmt.Fprintln(os.Stderr, "spawn:", err)
			os.Exit(1)
		}
		sess = s
	} else {
		p, err := xpty.NewPty(120, 30)
		if err != nil {
			fmt.Fprintln(os.Stderr, "pty:", err)
			os.Exit(1)
		}
		cmd := exec.Command(*shell)
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")
		if err := p.Start(cmd); err != nil {
			fmt.Fprintln(os.Stderr, "start:", err)
			os.Exit(1)
		}
		sess = p
	}
	defer sess.Close()

	fmt.Fprintf(w, "# %s wrap=%v shell=%s\n", time.Now().Format(time.RFC3339), *wrap, *shell)
	w.Flush()

	buf := make([]byte, 32*1024)
	reads := 0
	type chunk struct {
		buf []byte
		err error
	}
	ch := make(chan chunk, 16)
	go func() {
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, buf[:n])
				ch <- chunk{cp, nil}
			}
			if err != nil {
				ch <- chunk{nil, err}
				return
			}
		}
	}()

	var all []byte
	deadline := time.After(time.Duration(*secs) * time.Second)
	probeSent := false
	started := time.Now()
	for {
		if !probeSent && time.Since(started) > 500*time.Millisecond && *wrap {
			sess.Write([]byte(probe))
			probeSent = true
		}
		select {
		case c := <-ch:
			if c.buf != nil {
				reads++
				fmt.Fprintf(w, "--- read %d (%d bytes) ---\n", reads, len(c.buf))
				hexdump(w, c.buf)
				all = append(all, c.buf...)
				if binw != nil {
					binw.Write(c.buf)
				}
			}
			if c.err != nil {
				fmt.Fprintf(w, "--- read error: %v ---\n", c.err)
				goto done
			}
		case <-deadline:
			goto done
		}
	}
done:

	// Rune report: every non-ASCII rune in the raw stream, with its code
	// point, so corrupt vs intended glyphs are immediately visible.
	fmt.Fprintf(w, "\n=== rune report (%d bytes, %d reads) ===\n", len(all), reads)
	seen := map[rune]int{}
	for len(all) > 0 {
		r, sz := utf8.DecodeRune(all)
		if r == utf8.RuneError && sz == 1 {
			fmt.Fprintf(w, "INVALID-UTF8 byte 0x%02x\n", all[0])
			all = all[1:]
			continue
		}
		if r >= 0x80 {
			seen[r]++
		}
		all = all[sz:]
	}
	for r, c := range seen {
		fmt.Fprintf(w, "U+%04X %q x%d\n", r, r, c)
	}
	fmt.Fprintf(w, "=== end ===\n")
}

// hexdump writes buf in hex + ASCII form, one line per 16 bytes.
func hexdump(w io.Writer, buf []byte) {
	for off := 0; off < len(buf); off += 16 {
		end := off + 16
		if end > len(buf) {
			end = len(buf)
		}
		fmt.Fprintf(w, "%08x  ", off)
		for i := off; i < end; i++ {
			fmt.Fprintf(w, "%02x ", buf[i])
		}
		for i := end; i < off+16; i++ {
			fmt.Fprint(w, "   ")
		}
		fmt.Fprint(w, " |")
		for _, b := range buf[off:end] {
			if b >= 0x20 && b < 0x7f {
				fmt.Fprintf(w, "%c", b)
			} else {
				fmt.Fprint(w, ".")
			}
		}
		fmt.Fprintln(w, "|")
	}
}
