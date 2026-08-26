package config

import (
	"os"
	"time"
)

// Watch polls the config file for changes and calls cb with a freshly loaded
// config, or with a non-nil error if reloading failed (the previous config
// remains in effect). It returns a stop function.
func Watch(path string, interval time.Duration, cb func(*Config, error)) func() {
	stop := make(chan struct{})
	go func() {
		// Seed from the file as it stands right now: the caller has just
		// loaded and applied it, and zero baselines would fire a spurious
		// reload on the first tick.
		lastMod, lastSize := statFile(path)
		for {
			select {
			case <-stop:
				return
			case <-time.After(interval):
			mod, size := statFile(path)
			if mod.Equal(lastMod) && size == lastSize {
				continue
			}
			cfg, err := Load(path)
			newMod, newSize := statFile(path)
			// If the file was observed at zero bytes (e.g. in-place save
			// that truncates before rewriting), skip this cycle and don't
			// commit the stat so the next tick catches the completed write.
			if size == 0 || newSize == 0 {
				continue
			}
			if newMod.Equal(mod) && newSize == size {
				cb(cfg, err)
			}
			lastMod, lastSize = newMod, newSize
			}
		}
	}()
	return func() { close(stop) }
}

func statFile(path string) (time.Time, int64) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, -1
	}
	return info.ModTime(), info.Size()
}
