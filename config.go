package kaclaig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const toolName = "kaclaig"

// ConfigFile is the on-disk shape of an LLM config file. It is compatible
// with OpenCode config formats (V1 and V2), including JSONC.
type ConfigFile struct {
	// Providers is the V2 provider map.
	Providers map[string]ProviderConfig `json:"providers,omitempty"`
	// Provider is the legacy V1 provider map.
	Provider map[string]ProviderConfig `json:"provider,omitempty"`
	// Model selects the model (e.g. "provider/model" or V2 object form).
	Model ModelValue `json:"model,omitempty"`
}

// ProviderConfig describes one LLM provider block.
type ProviderConfig struct {
	Name     string                     `json:"name,omitempty"`
	Package  string                     `json:"package,omitempty"`
	Settings map[string]any             `json:"settings,omitempty"`
	Options  map[string]any             `json:"options,omitempty"` // V1 legacy
	Models   map[string]json.RawMessage `json:"models,omitempty"`
}

// ModelCfg holds optional per-model metadata.
type ModelCfg struct {
	Name  string `json:"name,omitempty"`
	Limit *struct {
		Context int `json:"context,omitempty"`
		Output  int `json:"output,omitempty"`
	} `json:"limit,omitempty"`
}

// ModelValue holds a parsed model selection.
type ModelValue struct {
	ProviderID string
	Model      string
	Variant    string
}

// UnmarshalJSON parses a model selection from a string ("provider/model",
// "provider/model#variant") or V2 object ({providerID, model, variant}).
func (m *ModelValue) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*m = ModelValue{}
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*m = parseModelString(s)
		return nil
	}
	var obj struct {
		ProviderID string `json:"providerID"`
		Model      string `json:"model"`
		Variant    string `json:"variant,omitempty"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	m.ProviderID = normalizeProviderID(obj.ProviderID)
	m.Model = obj.Model
	m.Variant = obj.Variant
	return nil
}

func parseModelString(s string) ModelValue {
	if s == "" {
		return ModelValue{}
	}
	slash := strings.Index(s, "/")
	if slash < 0 {
		return ModelValue{Model: s}
	}
	provider := s[:slash]
	rest := s[slash+1:]
	variant := ""
	if i := strings.Index(rest, "#"); i >= 0 {
		variant = rest[i+1:]
		rest = rest[:i]
	}
	return ModelValue{ProviderID: normalizeProviderID(provider), Model: rest, Variant: variant}
}

func normalizeProviderID(id string) string {
	switch id {
	case "azure-cognitive-services":
		return "azure"
	case "google-vertex-anthropic":
		return "google-vertex"
	default:
		return id
	}
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
//  3. $HOME/.config/opencode/opencode.json;
//  4. $HOME/.config/opencode/opencode.jsonc.
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
	return LLMSettingsFromBlob(data, path)
}

// LLMSettingsFromBlob parses an opencode-style config JSON/JSONC blob (the
// same format as a config file) and resolves LLM settings from it. If the blob
// does not resolve to a usable model, it returns LLMSettings{} (all empty)
// and no error. When label is non-empty it is included in error messages.
func LLMSettingsFromBlob(data []byte, label string) (LLMSettings, error) {
	if label == "" {
		label = "inline config"
	}
	var cfg ConfigFile
	if err := json.Unmarshal(stripJSONC(data), &cfg); err != nil {
		return LLMSettings{}, fmt.Errorf("parsing config %s: %w", label, err)
	}

	settings, ok := resolveSettings(&cfg)
	if !ok {
		return LLMSettings{}, nil
	}
	return settings, nil
}

// ParseLLMConfigBlob parses an opencode-style config JSON/JSONC blob (the
// same format as a config file) into a ConfigFile, validating that it is well
// formed. It does not resolve settings, so a blob without a usable model is
// accepted.
func ParseLLMConfigBlob(data []byte) (*ConfigFile, error) {
	var cfg ConfigFile
	if err := json.Unmarshal(stripJSONC(data), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
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
		filepath.Join(home, ".config", "opencode", "opencode.jsonc"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, true, nil
		}
	}
	return "", false, nil
}

// resolveSettings extracts base URL, API key, and model from the config.
// The model is expected as "<provider>/<model>" (optionally with #variant)
// or as a V2 object selection; a bare model name searches all providers.
func resolveSettings(cfg *ConfigFile) (LLMSettings, bool) {
	if cfg.Model.Model == "" && cfg.Model.ProviderID == "" {
		return LLMSettings{}, false
	}
	providers := mergedProviders(cfg)

	if cfg.Model.ProviderID != "" {
		prov, ok := lookupProvider(providers, cfg.Model.ProviderID)
		if !ok {
			return LLMSettings{}, false
		}
		s := LLMSettings{
			BaseURL: providerBaseURL(prov),
			APIKey:  providerAPIKey(prov),
			Model:   cfg.Model.Model,
		}
		return s, s.usable()
	}

	// Single name: search all providers for the model.
	for _, prov := range providers {
		if _, ok := prov.Models[cfg.Model.Model]; ok {
			s := LLMSettings{
				BaseURL: providerBaseURL(prov),
				APIKey:  providerAPIKey(prov),
				Model:   cfg.Model.Model,
			}
			return s, s.usable()
		}
	}
	return LLMSettings{}, false
}

func mergedProviders(cfg *ConfigFile) map[string]ProviderConfig {
	out := map[string]ProviderConfig{}
	for k, v := range cfg.Provider {
		out[normalizeProviderID(k)] = v
	}
	for k, v := range cfg.Providers {
		out[normalizeProviderID(k)] = v // native overrides legacy
	}
	return out
}

func lookupProvider(providers map[string]ProviderConfig, id string) (ProviderConfig, bool) {
	p, ok := providers[normalizeProviderID(id)]
	return p, ok
}

func providerBaseURL(p ProviderConfig) string {
	if v := stringValue(p.Settings, "baseURL"); v != "" {
		return v
	}
	return stringValue(p.Options, "baseURL")
}

func providerAPIKey(p ProviderConfig) string {
	if v := stringValue(p.Settings, "apiKey"); v != "" {
		return v
	}
	return stringValue(p.Options, "apiKey")
}

func stringValue(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func (s LLMSettings) usable() bool {
	return s.BaseURL != "" && s.Model != ""
}

// stripJSONC removes // and /* */ comments and trailing commas so that JSONC
// config files can be parsed by encoding/json.
func stripJSONC(data []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(data))
	inString := false
	inLineComment := false
	inBlockComment := false
	escape := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		next := byte(0)
		if i+1 < len(data) {
			next = data[i+1]
		}
		switch {
		case inLineComment:
			if c == '\n' {
				inLineComment = false
				out.WriteByte(c)
			}
		case inBlockComment:
			if c == '*' && next == '/' {
				inBlockComment = false
				i++
			}
		case inString:
			out.WriteByte(c)
			switch {
			case escape:
				escape = false
			case c == '\\':
				escape = true
			case c == '"':
				inString = false
			}
		case c == '"':
			inString = true
			out.WriteByte(c)
		case c == '/' && next == '/':
			inLineComment = true
			i++
		case c == '/' && next == '*':
			inBlockComment = true
			i++
		default:
			out.WriteByte(c)
		}
	}
	return stripTrailingCommas(out.Bytes())
}

func stripTrailingCommas(data []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(data))
	inString := false
	escape := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			out.WriteByte(c)
			switch {
			case escape:
				escape = false
			case c == '\\':
				escape = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(data) {
				switch data[j] {
				case ' ', '\t', '\n', '\r':
					j++
					continue
				}
				break
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue
			}
		}
		out.WriteByte(c)
	}
	return out.Bytes()
}
