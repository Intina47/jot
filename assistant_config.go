package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type AssistantConfig struct {
	Provider           string `json:"provider"`
	Model              string `json:"model"`
	OllamaURL          string `json:"ollamaUrl"`
	OllamaAPIKey       string `json:"ollamaApiKey,omitempty"`
	OpenAIKey          string `json:"openaiKey,omitempty"`
	AnthropicKey       string `json:"anthropicKey,omitempty"`
	GmailTokenPath     string `json:"gmailTokenPath"`
	GmailCredPath      string `json:"gmailCredPath"`
	BrowserProfilePath string `json:"browserProfilePath"`
	BrowserEnabled     bool   `json:"browserEnabled"`
	BrowserOnboarded   bool   `json:"browserOnboarded"`
	BrowserConnected   bool   `json:"browserConnected"`
	AttachmentSaveDir  string `json:"attachmentSaveDir"`
	DefaultFormat      string `json:"defaultFormat"`
	ConfirmByDefault   bool   `json:"confirmByDefault"`
	Verbose            bool   `json:"verbose"`
}

type AssistantConfigOverrides struct {
	Provider           string
	Model              string
	OllamaURL          string
	OllamaAPIKey       string
	OpenAIKey          string
	AnthropicKey       string
	GmailTokenPath     string
	GmailCredPath      string
	BrowserProfilePath string
	AttachmentSaveDir  string
	DefaultFormat      string
	Verbose            *bool
	ConfirmByDefault   *bool
}

func LoadAssistantConfig(overrides AssistantConfigOverrides) (AssistantConfig, error) {
	configDir, err := assistantConfigDir()
	if err != nil {
		return AssistantConfig{}, err
	}

	cfg := defaultAssistantConfig(configDir)
	if err := loadJSONFileIfExists(filepath.Join(configDir, "assistant.json"), &cfg); err != nil {
		return AssistantConfig{}, err
	}
	applyAssistantEnv(&cfg)
	applyAssistantOverrides(&cfg, overrides)
	normalizeAssistantConfig(&cfg, configDir)
	return cfg, nil
}

func assistantConfigDir() (string, error) {
	if dir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(dir) != "" {
		return filepath.Join(dir, "jot"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			return filepath.Join(appData, "jot"), nil
		}
		return filepath.Join(home, "AppData", "Roaming", "jot"), nil
	}
	return filepath.Join(home, ".config", "jot"), nil
}

func assistantConfigPath() (string, error) {
	dir, err := assistantConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "assistant.json"), nil
}

func defaultAssistantConfig(configDir string) AssistantConfig {
	tokenPath, credPath := gmailAuthPaths(configDir)
	browserProfilePath, _ := defaultBrowserProfileDir()
	return AssistantConfig{
		Provider:           "ollama",
		Model:              "llama3.2",
		OllamaURL:          "http://localhost:11434",
		GmailTokenPath:     tokenPath,
		GmailCredPath:      credPath,
		BrowserProfilePath: browserProfilePath,
		AttachmentSaveDir:  filepath.Join(configDir, "attachments"),
		DefaultFormat:      "text",
		ConfirmByDefault:   true,
		Verbose:            false,
	}
}

func gmailAuthPaths(configDir string) (string, string) {
	return filepath.Join(configDir, "gmail_token.json"), filepath.Join(configDir, "gmail_credentials.json")
}

func applyAssistantEnv(cfg *AssistantConfig) {
	if value := strings.TrimSpace(os.Getenv("JOT_ASSISTANT_PROVIDER")); value != "" {
		cfg.Provider = value
	}
	if value := strings.TrimSpace(os.Getenv("JOT_ASSISTANT_MODEL")); value != "" {
		cfg.Model = value
	}
	if value := strings.TrimSpace(os.Getenv("JOT_ASSISTANT_OLLAMA_URL")); value != "" {
		cfg.OllamaURL = value
	}
	if value := strings.TrimSpace(os.Getenv("JOT_ASSISTANT_OLLAMA_API_KEY")); value != "" {
		cfg.OllamaAPIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("OLLAMA_API_KEY")); value != "" {
		cfg.OllamaAPIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); value != "" {
		cfg.OpenAIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); value != "" {
		cfg.AnthropicKey = value
	}
}

func applyAssistantOverrides(cfg *AssistantConfig, overrides AssistantConfigOverrides) {
	if value := strings.TrimSpace(overrides.Provider); value != "" {
		cfg.Provider = value
	}
	if value := strings.TrimSpace(overrides.Model); value != "" {
		cfg.Model = value
	}
	if value := strings.TrimSpace(overrides.OllamaURL); value != "" {
		cfg.OllamaURL = value
	}
	if value := strings.TrimSpace(overrides.OllamaAPIKey); value != "" {
		cfg.OllamaAPIKey = value
	}
	if value := strings.TrimSpace(overrides.OpenAIKey); value != "" {
		cfg.OpenAIKey = value
	}
	if value := strings.TrimSpace(overrides.AnthropicKey); value != "" {
		cfg.AnthropicKey = value
	}
	if value := strings.TrimSpace(overrides.GmailTokenPath); value != "" {
		cfg.GmailTokenPath = value
	}
	if value := strings.TrimSpace(overrides.GmailCredPath); value != "" {
		cfg.GmailCredPath = value
	}
	if value := strings.TrimSpace(overrides.BrowserProfilePath); value != "" {
		cfg.BrowserProfilePath = value
	}
	if value := strings.TrimSpace(overrides.AttachmentSaveDir); value != "" {
		cfg.AttachmentSaveDir = value
	}
	if value := strings.TrimSpace(overrides.DefaultFormat); value != "" {
		cfg.DefaultFormat = value
	}
	if overrides.Verbose != nil {
		cfg.Verbose = *overrides.Verbose
	}
	if overrides.ConfirmByDefault != nil {
		cfg.ConfirmByDefault = *overrides.ConfirmByDefault
	}
}

func normalizeAssistantConfig(cfg *AssistantConfig, configDir string) {
	cfg.Provider = strings.ToLower(strings.TrimSpace(cfg.Provider))
	if cfg.Provider == "" {
		cfg.Provider = "ollama"
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.Model == "" {
		cfg.Model = "llama3.2"
	}
	cfg.OllamaURL = strings.TrimRight(strings.TrimSpace(cfg.OllamaURL), "/")
	if cfg.OllamaURL == "" {
		cfg.OllamaURL = "http://localhost:11434"
	}
	cfg.DefaultFormat = strings.ToLower(strings.TrimSpace(cfg.DefaultFormat))
	if cfg.DefaultFormat != "json" {
		cfg.DefaultFormat = "text"
	}
	if strings.TrimSpace(cfg.GmailTokenPath) == "" || strings.TrimSpace(cfg.GmailCredPath) == "" {
		tokenPath, credPath := gmailAuthPaths(configDir)
		if strings.TrimSpace(cfg.GmailTokenPath) == "" {
			cfg.GmailTokenPath = tokenPath
		}
		if strings.TrimSpace(cfg.GmailCredPath) == "" {
			cfg.GmailCredPath = credPath
		}
	}
	if strings.TrimSpace(cfg.BrowserProfilePath) == "" {
		if browserProfilePath, err := defaultBrowserProfileDir(); err == nil {
			cfg.BrowserProfilePath = browserProfilePath
		} else {
			cfg.BrowserProfilePath = filepath.Join(configDir, "browser-profile")
		}
	}
	if strings.TrimSpace(cfg.AttachmentSaveDir) == "" {
		cfg.AttachmentSaveDir = filepath.Join(configDir, "attachments")
	}
}

func LoadAssistantToken(path string, dst any) error {
	return loadJSONFile(path, dst)
}

func SaveAssistantToken(path string, value any) error {
	return writeSecureJSON(path, value)
}

func SaveAssistantConfigFile(cfg AssistantConfig) error {
	path, err := assistantConfigPath()
	if err != nil {
		return err
	}
	return writeSecureJSON(path, cfg)
}

func loadJSONFileIfExists(path string, dst any) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return loadJSONFile(path, dst)
}

func loadJSONFile(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data = trimUTF8BOM(data)
	return json.Unmarshal(data, dst)
}

func trimUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func writeSecureJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tempFile, err := os.CreateTemp(filepath.Dir(path), "assistant-*.json")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if err := tempFile.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		_ = tempFile.Close()
		return err
	}
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, 0o600); err != nil && runtime.GOOS != "windows" {
		return err
	}
	return os.Rename(tempPath, path)
}
