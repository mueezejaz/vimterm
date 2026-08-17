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
		var lastMod time.Time
		for {
			select {
			case <-stop:
				return
			case <-time.After(interval):
				info, err := os.Stat(path)
				if err != nil {
					continue
				}
				if !info.ModTime().Equal(lastMod) {
					lastMod = info.ModTime()
					cfg, err := Load(path)
					cb(cfg, err)
				}
			}
		}
	}()
	return func() { close(stop) }
}