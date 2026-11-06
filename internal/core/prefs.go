package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"sonidex/backend"
)

// Prefs are the few settings that persist between runs. They live in a small
// JSON file under the user config dir (no registry, no toolkit store).
type Prefs struct {
	Profile         string `json:"profile"`
	SilenceSuppress bool   `json:"silence_suppress"`

	mu   sync.Mutex
	path string
}

func prefsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "sonidex", "prefs.json")
}

// LoadPrefs reads the saved settings, falling back to the backend defaults.
func LoadPrefs() *Prefs {
	p := &Prefs{
		Profile:         backend.ProfileNames[backend.ProfileLowLatency],
		SilenceSuppress: backend.DefaultStreamOptions().SuppressSilence,
		path:            prefsPath(),
	}
	if p.path != "" {
		if b, err := os.ReadFile(p.path); err == nil {
			_ = json.Unmarshal(b, p)
		}
	}
	if backend.ProfileFromName(p.Profile).String() != p.Profile {
		p.Profile = backend.ProfileNames[backend.ProfileLowLatency]
	}
	return p
}

// Save writes the settings; failures are ignored (prefs are best-effort).
func (p *Prefs) Save() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.path == "" {
		return
	}
	b, err := json.Marshal(struct {
		Profile         string `json:"profile"`
		SilenceSuppress bool   `json:"silence_suppress"`
	}{p.Profile, p.SilenceSuppress})
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(p.path), 0o755) == nil {
		_ = os.WriteFile(p.path, b, 0o644)
	}
}
