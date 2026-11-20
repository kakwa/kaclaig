package kaclaig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleConfig = `{
  "$schema": "https://opencode.ai/config.json",
  "providers": {
    "local": {
      "name": "Local AI",
      "package": "@opencode/ai/providers/openai-compatible",
      "settings": {
        "baseURL": "https://ai.example.com/v1",
        "apiKey": "asd"
      },
      "models": {
        "qwen3.8-27b": {
          "name": "Qwen 3.8 27B",
          "limit": { "context": 90000, "output": 16384 }
        }
      }
    }
  },
  "model": "local/qwen3.8_27B"
}`

func TestLoadLLMSettingsExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, []byte(sampleConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadLLMSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	want := LLMSettings{
		BaseURL: "https://ai.example.com/v1",
		APIKey:  "asd",
		Model:   "qwen3.8_27B",
	}
	if s != want {
		t.Errorf("LoadLLMSettings = %+v, want %+v", s, want)
	}
}

func TestLoadLLMSettingsExplicitMissing(t *testing.T) {
	if _, err := LoadLLMSettings("/nonexistent/path/config.json"); err == nil {
		t.Error("expected error for explicit missing file")
	}
}

func TestLoadLLMSettingsHomeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, "."+toolName+"-llm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, toolName+".json"), []byte(sampleConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadLLMSettings("")
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "qwen3.8_27B" || s.BaseURL != "https://ai.example.com/v1" || s.APIKey != "asd" {
		t.Errorf("LoadLLMSettings = %+v, want tool-specific config", s)
	}
}

func TestLoadLLMSettingsOpencodeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(sampleConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadLLMSettings("")
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "qwen3.8_27B" {
		t.Errorf("LoadLLMSettings = %+v, want opencode fallback config", s)
	}
}

func TestLoadLLMSettingsOpencodeJSONCFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{
  "providers": { "p": { "settings": { "baseURL": "https://x/v1", "apiKey": "k" }, "models": { "m": {} } } },
  "model": "p/m",
}`
	if err := os.WriteFile(filepath.Join(dir, "opencode.jsonc"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadLLMSettings("")
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "m" || s.BaseURL != "https://x/v1" || s.APIKey != "k" {
		t.Errorf("LoadLLMSettings = %+v, want opencode jsonc fallback config", s)
	}
}

func TestLoadLLMSettingsPriority(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Tool-specific config.
	dir := filepath.Join(home, "."+toolName+"-llm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, toolName+".json"), []byte(sampleConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	// Opencode config with a different value.
	dir2 := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	other := strings.Replace(sampleConfig, "qwen3.8_27B", "other-model", 1)
	other = strings.Replace(other, "https://ai.example.com/v1", "https://other.example.com/v1", 1)
	if err := os.WriteFile(filepath.Join(dir2, "opencode.json"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadLLMSettings("")
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "qwen3.8_27B" || s.BaseURL != "https://ai.example.com/v1" {
		t.Errorf("tool-specific config should win, got %+v", s)
	}
}

func TestLoadLLMSettingsNoFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s, err := LoadLLMSettings("")
	if err != nil {
		t.Fatal(err)
	}
	if s != (LLMSettings{}) {
		t.Errorf("expected empty settings, got %+v", s)
	}
}

func TestLoadLLMSettingsBadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLLMSettings(path); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadLLMSettingsJSONC(t *testing.T) {
	cfg := `{
  // OpenCode-style JSONC config
  "providers": {
    "local": {
      "settings": {
        "baseURL": "https://ai.example.com/v1", // inline comment
        "apiKey": "asd",
      },
      "models": {
        "m": {},
      },
    },
  },
  "model": "local/m",
}`
	path := filepath.Join(t.TempDir(), "opencode.jsonc")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadLLMSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	want := LLMSettings{BaseURL: "https://ai.example.com/v1", APIKey: "asd", Model: "m"}
	if s != want {
		t.Errorf("got %+v, want %+v", s, want)
	}
}

func TestParseLLMConfigBlob(t *testing.T) {
	cfg, err := ParseLLMConfigBlob([]byte(sampleConfig))
	if err != nil {
		t.Fatal(err)
	}
	s, ok := resolveSettings(cfg)
	if !ok {
		t.Fatal("expected the parsed blob to resolve to settings")
	}
	if s.Model != "qwen3.8_27B" || s.BaseURL != "https://ai.example.com/v1" || s.APIKey != "asd" {
		t.Errorf("resolveSettings = %+v, want the sample settings", s)
	}

	if _, err := ParseLLMConfigBlob([]byte("{not json")); err == nil {
		t.Error("expected error for invalid JSON")
	}

	// A blob without a usable model still parses: it is only malformed
	// JSON that is an error.
	if _, err := ParseLLMConfigBlob([]byte(`{}`)); err != nil {
		t.Errorf("expected {} to parse, got: %v", err)
	}
}

func TestLLMSettingsFromBlob(t *testing.T) {
	s, err := LLMSettingsFromBlob([]byte(sampleConfig), "")
	if err != nil {
		t.Fatal(err)
	}
	want := LLMSettings{
		BaseURL: "https://ai.example.com/v1",
		APIKey:  "asd",
		Model:   "qwen3.8_27B",
	}
	if s != want {
		t.Errorf("LLMSettingsFromBlob = %+v, want %+v", s, want)
	}

	// A blob that does not resolve to a usable model is not an error.
	s, err = LLMSettingsFromBlob([]byte(`{}`), "inline")
	if err != nil {
		t.Fatal(err)
	}
	if s != (LLMSettings{}) {
		t.Errorf("expected empty settings for an unusable blob, got %+v", s)
	}

	if _, err := LLMSettingsFromBlob([]byte("{not json"), "inline"); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadLLMSettingsV1ProviderOptions(t *testing.T) {
	cfg := `{
  "provider": {
    "openai": {
      "options": {
        "baseURL": "https://api.example.com/v1",
        "apiKey": "sk-test"
      },
      "models": { "gpt-4o": {} }
    }
  },
  "model": "openai/gpt-4o"
}`
	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadLLMSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	want := LLMSettings{BaseURL: "https://api.example.com/v1", APIKey: "sk-test", Model: "gpt-4o"}
	if s != want {
		t.Errorf("got %+v, want %+v", s, want)
	}
}

func TestLoadLLMSettingsModelObject(t *testing.T) {
	cfg := `{
  "providers": {
    "openai": {
      "settings": { "baseURL": "https://api.example.com/v1", "apiKey": "k" },
      "models": { "gpt-4o": {} }
    }
  },
  "model": { "providerID": "openai", "model": "gpt-4o" }
}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadLLMSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "gpt-4o" || s.BaseURL != "https://api.example.com/v1" || s.APIKey != "k" {
		t.Errorf("got %+v", s)
	}
}

func TestLoadLLMSettingsModelVariantString(t *testing.T) {
	cfg := `{
  "providers": {
    "p": { "settings": { "baseURL": "https://x/v1", "apiKey": "k" }, "models": { "m": {} } }
  },
  "model": "p/m#fast"
}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadLLMSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "m" || s.BaseURL != "https://x/v1" {
		t.Errorf("got %+v", s)
	}
}

func TestLoadLLMSettingsProviderIDNormalization(t *testing.T) {
	cfg := `{
  "provider": {
    "azure-cognitive-services": {
      "options": { "baseURL": "https://az.example/v1", "apiKey": "k" },
      "models": { "gpt-4o": {} }
    }
  },
  "model": "azure-cognitive-services/gpt-4o"
}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadLLMSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.BaseURL != "https://az.example/v1" || s.Model != "gpt-4o" {
		t.Errorf("got %+v", s)
	}
}

func TestLoadLLMSettingsNativeProvidersOverrideLegacy(t *testing.T) {
	cfg := `{
  "provider": {
    "p": {
      "options": { "baseURL": "https://old/v1", "apiKey": "old" },
      "models": { "m": {} }
    }
  },
  "providers": {
    "p": {
      "settings": { "baseURL": "https://new/v1", "apiKey": "new" },
      "models": { "m": {} }
    }
  },
  "model": "p/m"
}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadLLMSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.BaseURL != "https://new/v1" || s.APIKey != "new" {
		t.Errorf("got %+v", s)
	}
}

func TestLoadLLMSettingsSettingsOverrideOptions(t *testing.T) {
	cfg := `{
  "providers": {
    "p": {
      "options": { "baseURL": "https://opt/v1", "apiKey": "opt" },
      "settings": { "baseURL": "https://set/v1" },
      "models": { "m": {} }
    }
  },
  "model": "p/m"
}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadLLMSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.BaseURL != "https://set/v1" || s.APIKey != "opt" {
		t.Errorf("got %+v", s)
	}
}

func TestLoadLLMSettingsBareModelSearch(t *testing.T) {
	cfg := `{
  "providers": {
    "p": { "settings": { "baseURL": "https://p/v1", "apiKey": "pk" }, "models": { "x": {} } },
    "q": { "settings": { "baseURL": "https://q/v1", "apiKey": "qk" }, "models": { "y": {} } }
  },
  "model": "y"
}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadLLMSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.BaseURL != "https://q/v1" || s.APIKey != "qk" || s.Model != "y" {
		t.Errorf("got %+v", s)
	}
}

func TestResolveSettingsNoModel(t *testing.T) {
	cfg := &ConfigFile{}
	if _, ok := resolveSettings(cfg); ok {
		t.Error("expected no settings without a model")
	}
}

func TestResolveSettingsUnknownProvider(t *testing.T) {
	cfg := &ConfigFile{Model: parseModelString("nosuch/model")}
	if _, ok := resolveSettings(cfg); ok {
		t.Error("expected no settings for unknown provider")
	}
}

func TestNewLLMFromSettings(t *testing.T) {
	s := LLMSettings{BaseURL: "https://x.example/v1", APIKey: "k", Model: "m"}
	cfg := LLMConfigFromSettings(s)
	llm, err := NewLLM(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if llm.baseURL != "https://x.example/v1" || llm.apiKey != "k" || llm.model != "m" {
		t.Errorf("NewLLM got wrong settings: %+v", llm)
	}
}
