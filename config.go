package kaclaig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const toolName = "kaclaig"

// ConfigFile is the on-disk shape of an LLM config file. It is compatible
// with the opencode.json provider config format.
type ConfigFile struct {
	Providers map[string]ProviderConfig `json:"providers,omitempty"`
	Model     string                    `json:"model,omitempty"`
}

// ProviderConfig describes one LLM provider block.
type ProviderConfig struct {
	Name     string              `json:"name,omitempty"`
	Package  string              `json:"package,omitempty"`
	Settings ProviderSettings    `json:"settings,omitempty"`
	Models   map[string]ModelCfg `json:"models,omitempty"`
}

// ProviderSettings holds connection settings for a provider.
type ProviderSettings struct {
	BaseURL string `json:"baseURL,omitempty"`
	APIKey  string `json:"apiKey,omitempty"`
}

// ModelCfg holds optional per-model metadata.
type ModelCfg struct {
	Name  string `json:"name,omitempty"`
	Limit *struct {
		Context int `json:"context,omitempty"`
		Output  int `json:"output,omitempty"`
	} `json:"limit,omitempty"`
}

// LLMSettings is the resolved connection settings for an LLM client.
type LLMSettings struct {
	BaseURL string
	APIKey  string
	Model   string
}

// LLMConfigFromSettings converts resolved settings into an LLMConfig.
// Zero-valued fields fall back to environment variables inside NewLLM.
func LLMConfigFromSettings(s LLMSettings) LLMConfig {
	return LLMConfig{BaseURL: s.BaseURL, APIKey: s.APIKey, Model: s.Model}
}

// LoadLLMSettings resolves LLM settings from an opencode-style config file,
// looking in (in order):
//  1. the explicit path, if non-empty (required when provided);
//  2. $HOME/.kaclaig-llm/kaclaig.json;
//  3. $HOME/.config/opencode/opencode.json.
//
// If no file is found, or the file does not resolve to a usable model, it
// returns LLMSettings{} (all empty) and no error.
func LoadLLMSettings(explicit string) (LLMSettings, error) {
	path, ok, err := findConfigFile(explicit)
	if err != nil {
		return LLMSettings{}, err
	}
	if !ok {
		return LLMSettings{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return LLMSettings{}, fmt.Errorf("reading config %s: %w", path, err)
	}
	var cfg ConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return LLMSettings{}, fmt.Errorf("parsing config %s: %w", path, err)
	}

	settings, ok := resolveSettings(&cfg)
	if !ok {
		return LLMSettings{}, nil
	}
	return settings, nil
}

// findConfigFile locates the config file to use.
func findConfigFile(explicit string) (string, bool, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", false, fmt.Errorf("config file not found: %s", explicit)
		}
		return explicit, true, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, nil
	}

	candidates := []string{
		filepath.Join(home, "."+toolName+"-llm", toolName+".json"),
		filepath.Join(home, ".config", "opencode", "opencode.json"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, true, nil
		}
	}
	return "", false, nil
}

// resolveSettings extracts base URL, API key, and model from the config.
// The model string is expected in the form "<provider>/<model>".
func resolveSettings(cfg *ConfigFile) (LLMSettings, bool) {
	if cfg.Model == "" {
		return LLMSettings{}, false
	}

	parts := strings.SplitN(cfg.Model, "/", 2)
	if len(parts) != 2 {
		// Single name: search all providers for the model.
		for _, prov := range cfg.Providers {
			if _, ok := prov.Models[cfg.Model]; ok {
				s := LLMSettings{
					BaseURL: prov.Settings.BaseURL,
					APIKey:  prov.Settings.APIKey,
					Model:   cfg.Model,
				}
				return s, s.usable()
			}
		}
		return LLMSettings{}, false
	}

	provider, model := parts[0], parts[1]
	prov, ok := cfg.Providers[provider]
	if !ok {
		return LLMSettings{}, false
	}
	_ = prov.Models // model metadata is informational only

	s := LLMSettings{
		BaseURL: prov.Settings.BaseURL,
		APIKey:  prov.Settings.APIKey,
		Model:   model,
	}
	return s, s.usable()
}

func (s LLMSettings) usable() bool {
	return s.BaseURL != "" && s.Model != ""
}
