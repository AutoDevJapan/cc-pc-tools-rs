package transport

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Config is encrypted with owner-scoped DPAPI on Windows. It must never be
// formatted into an error, logged, or passed as a command-line argument.
type Config struct {
	BaseURL   string `json:"baseURL"`
	SyncToken string `json:"syncToken"`
	ClientID  string `json:"clientId"`
	CachePath string `json:"cachePath"`
}

func (c Config) Validate() error {
	if _, err := NewClient(c.BaseURL, c.SyncToken); err != nil {
		return err
	}
	if c.ClientID == "" || !filepath.IsAbs(c.CachePath) || filepath.Base(c.CachePath) != "todo.json" {
		return errors.New("todo_remote_invalid_configuration")
	}
	return nil
}
func SaveConfig(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := protect(wire(c))
	if err != nil {
		return err
	}
	return atomicWrite(path, b)
}
func LoadConfig(path string) (Config, error) {
	b, err := readFileLimit(path, 64<<10)
	if err != nil {
		return Config{}, errors.New("todo_remote_configuration_unavailable")
	}
	b, err = unprotect(path, b)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if json.Unmarshal(b, &c) != nil {
		return Config{}, errors.New("todo_remote_invalid_configuration")
	}
	return c, c.Validate()
}

// Provision installs no activation marker. The caller activates all updated
// readers/writers together, after deploying the helper and verifying the cloud.
func Provision(path string, input *os.File) error {
	var c Config
	if json.NewDecoder(input).Decode(&c) != nil {
		return errors.New("todo_remote_invalid_configuration")
	}
	return SaveConfig(path, c)
}
