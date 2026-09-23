package api

import "github.com/laststate/trace/internal/config"

// configWithOpenUI builds a minimal config for tests that only flip OpenUI.
// All other fields are zero-valued, which is fine for tests that do not
// exercise them.
func configWithOpenUI(open bool) config.Config {
	return config.Config{OpenUI: open}
}
