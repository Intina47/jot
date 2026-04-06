package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

type assistantInvocation struct {
	Config     AssistantConfig
	Format     string
	Verbose    bool
	NoConfirm  bool
	UseUI      bool
	Onboarding bool
	Scope      string
	Prompt     string
	Command    string
	Args       []string
	Help       bool
}

type assistantStatusSnapshot struct {
	Provider          string   `json:"provider"`
	Model             string   `json:"model"`
	ProviderAvailable bool     `json:"providerAvailable"`
	Format            string   `json:"format"`
	Verbose           bool     `json:"verbose"`
	Scope             string   `json:"scope,omitempty"`
	GmailConnected    bool     `json:"gmailConnected"`
	GmailEmail        string   `json:"gmailEmail,omitempty"`
	GmailSendReady    bool     `json:"gmailSendReady,omitempty"`
	BrowserEnabled    bool     `json:"browserEnabled,omitempty"`
	BrowserConnected  bool     `json:"browserConnected,omitempty"`
	BrowserProfile    string   `json:"browserProfile,omitempty"`
	ConnectedChannels []string `json:"connectedChannels,omitempty"`
}

type NormalizedEmail struct {
	ID          string
	ThreadID    string
	Subject     string
	From        string
	To          []string
	Date        time.Time
	BodyText    string
	BodyHTML    string
	Snippet     string
	Labels      []string
	Links       []EmailLink
	Attachments []AttachmentMeta
	Unread      bool
}

type AssistantSemanticSummary struct {
	Summary string                    `json:"summary"`
	Actions []AssistantSemanticAction `json:"actions,omitempty"`
}

type AssistantSemanticAction struct {
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	EmailID  string `json:"emailId,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Priority string `json:"priority,omitempty"`
}

func jotAssistant(stdin io.Reader, stdout io.Writer, args []string, now func() time.Time) error {
	if now == nil {
		now = time.Now
	}

	inv, err := parseAssistantInvocation(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return writeHelp(stdout, "assistant")
		}
		return err
	}
	if inv.Help {
		return writeHelp(stdout, "assistant")
	}

	switch inv.Command {
	case "auth":
		return runAssistantAuth(stdin, stdout, inv)
	case "status":
		return runAssistantStatus(stdout, inv)
	case "browser":
		return runAssistantBrowser(stdin, stdout, inv)
	case "channels":
		return runAssistantChannels(stdin, stdout, inv)
	case "gmail":
		return runAssistantGmail(stdin, stdout, inv, now)
	}

	performedOnboarding := false
	if inv.Onboarding || assistantNeedsOnboarding(inv.Config) {
		inv, performedOnboarding, err = runAssistantOnboarding(stdin, stdout, inv, now)
		if err != nil {
			return err
		}
	}

	session, err := newAssistantSession(inv)
	if err != nil {
		return err
	}

	if performedOnboarding && strings.TrimSpace(inv.Prompt) == "" && !inv.UseUI {
		if err := runAssistantWelcomeSummary(stdout, session, now); err != nil {
			_, _ = fmt.Fprintf(stdout, "setup finished, but the welcome summary failed: %v\n\n", err)
		}
	}

	if inv.UseUI {
		return runAssistantViewer(stdout, session, inv, now)
	}
	if strings.TrimSpace(inv.Prompt) != "" {
		return runAssistantSingleShot(stdin, stdout, session, inv, now)
	}
	return runAssistantInteractive(stdin, stdout, session, inv, now)
}

func parseAssistantInvocation(args []string) (assistantInvocation, error) {
	cfg, err := LoadAssistantConfig(AssistantConfigOverrides{})
	if err != nil {
		return assistantInvocation{}, err
	}
	inv := assistantInvocation{
		Config: cfg,
		Format: cfg.DefaultFormat,
	}

	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(positional) > 0 {
			positional = append(positional, args[i:]...)
			break
		}
		if arg == "-h" || arg == "--help" {
			inv.Help = true
			continue
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}

		name, value, hasValue := strings.Cut(arg, "=")
		switch name {
		case "--provider":
			value, i, err = assistantFlagValue(args, i, value, hasValue, name)
			if err != nil {
				return inv, err
			}
			inv.Config.Provider = strings.TrimSpace(value)
		case "--model":
			value, i, err = assistantFlagValue(args, i, value, hasValue, name)
			if err != nil {
				return inv, err
			}
			inv.Config.Model = strings.TrimSpace(value)
		case "--format":
			value, i, err = assistantFlagValue(args, i, value, hasValue, name)
			if err != nil {
				return inv, err
			}
			value = strings.ToLower(strings.TrimSpace(value))
			if value != "text" && value != "json" {
				return inv, fmt.Errorf("unknown format %q", value)
			}
			inv.Format = value
		case "--cap":
			value, i, err = assistantFlagValue(args, i, value, hasValue, name)
			if err != nil {
				return inv, err
			}
			inv.Scope = strings.ToLower(strings.TrimSpace(value))
		case "--verbose":
			inv.Verbose = true
		case "--no-confirm":
			inv.NoConfirm = true
		case "--ui":
			inv.UseUI = true
		case "--onboarding":
			inv.Onboarding = true
		default:
			return inv, fmt.Errorf("unknown assistant flag %s", arg)
		}
	}

	inv.Config.Verbose = inv.Verbose
	if inv.NoConfirm {
		inv.Config.ConfirmByDefault = false
	}
	if inv.Format == "" {
		inv.Format = inv.Config.DefaultFormat
	}
	inv.Config.DefaultFormat = inv.Format

	if len(positional) == 0 {
		return inv, nil
	}
	first := strings.ToLower(strings.TrimSpace(positional[0]))
	switch first {
	case "auth", "status", "gmail", "browser", "channels":
		inv.Command = first
		if len(positional) > 1 {
			inv.Args = append(inv.Args, positional[1:]...)
		}
	default:
		inv.Prompt = strings.TrimSpace(strings.Join(positional, " "))
	}
	return inv, nil
}

func assistantFlagValue(args []string, index int, value string, hasValue bool, name string) (string, int, error) {
	if hasValue {
		return value, index, nil
	}
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("missing value for %s", name)
	}
	return args[index+1], index + 1, nil
}

func assistantNeedsOnboarding(cfg AssistantConfig) bool {
	if !assistantProviderConfigured(cfg) {
		return true
	}
	if !assistantGmailTokenExists(cfg) {
		return true
	}
	if !cfg.BrowserOnboarded {
		return true
	}
	if cfg.BrowserEnabled && !assistantBrowserProfileExists(cfg) {
		return true
	}
	return false
}

func assistantProviderConfigured(cfg AssistantConfig) bool {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	model := strings.TrimSpace(cfg.Model)
	if provider == "" || model == "" {
		return false
	}
	switch provider {
	case "ollama":
		if assistantOllamaLikelyRemote(cfg) {
			return strings.TrimSpace(cfg.OllamaAPIKey) != ""
		}
		return true
	default:
		return false
	}
}

func assistantOllamaLikelyRemote(cfg AssistantConfig) bool {
	baseURL := strings.ToLower(strings.TrimSpace(cfg.OllamaURL))
	if baseURL == "" {
		return false
	}
	return !strings.Contains(baseURL, "localhost") && !strings.Contains(baseURL, "127.0.0.1")
}

func assistantGmailTokenExists(cfg AssistantConfig) bool {
	path := strings.TrimSpace(cfg.GmailTokenPath)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func assistantGmailCredentialsExist(cfg AssistantConfig) bool {
	path := strings.TrimSpace(cfg.GmailCredPath)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func assistantBrowserProfileExists(cfg AssistantConfig) bool {
	path := strings.TrimSpace(cfg.BrowserProfilePath)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func runAssistantOnboarding(stdin io.Reader, stdout io.Writer, inv assistantInvocation, now func() time.Time) (assistantInvocation, bool, error) {
	if now == nil {
		now = time.Now
	}
	ui := newTermUI(stdout)
	if _, err := fmt.Fprint(stdout, ui.header("Assistant Setup")); err != nil {
		return inv, false, err
	}
	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("first run setup for model access, Gmail auth, the browser computer, optional messaging channels, and a quick inbox summary.")); err != nil {
		return inv, false, err
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return inv, false, err
	}

	cfg, err := assistantOnboardProvider(stdin, stdout, inv.Config)
	if err != nil {
		return inv, false, err
	}
	cfg, err = assistantOnboardGmail(stdin, stdout, cfg)
	if err != nil {
		return inv, false, err
	}
	cfg, err = assistantOnboardBrowser(stdin, stdout, cfg)
	if err != nil {
		return inv, false, err
	}
	cfg, err = assistantOnboardChannels(stdin, stdout, cfg)
	if err != nil {
		return inv, false, err
	}
	if err := SaveAssistantConfigFile(cfg); err != nil {
		return inv, false, err
	}

	if _, err := fmt.Fprintln(stdout, ui.success("setup complete")); err != nil {
		return inv, false, err
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return inv, false, err
	}

	inv.Config = cfg
	inv.Onboarding = false
	return inv, true, nil
}

func assistantOnboardProvider(stdin io.Reader, stdout io.Writer, cfg AssistantConfig) (AssistantConfig, error) {
	ui := newTermUI(stdout)
	reader := bufio.NewReader(stdin)

	for {
		if _, err := fmt.Fprintln(stdout, ui.tbold("  1. model provider")); err != nil {
			return cfg, err
		}
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("available now: ollama")); err != nil {
			return cfg, err
		}
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("coming soon: openai, anthropic")); err != nil {
			return cfg, err
		}

		provider, err := assistantPromptLine(reader, stdout, "provider", assistantOnboardingDefaultProvider(cfg))
		if err != nil {
			return cfg, err
		}
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			provider = "ollama"
		}
		if provider != "ollama" {
			if _, err := fmt.Fprintln(stdout, "  "+ui.tyellow("only ollama is available in this build right now.")); err != nil {
				return cfg, err
			}
			if _, err := fmt.Fprintln(stdout); err != nil {
				return cfg, err
			}
			continue
		}

		baseURL, err := assistantPromptLine(reader, stdout, "ollama url", assistantDefaultString(cfg.OllamaURL, "http://localhost:11434"))
		if err != nil {
			return cfg, err
		}
		model, err := assistantPromptLine(reader, stdout, "model", assistantDefaultString(cfg.Model, "llama3.2"))
		if err != nil {
			return cfg, err
		}

		apiPrompt := "ollama api key (leave blank for local ollama)"
		defaultKey := strings.TrimSpace(cfg.OllamaAPIKey)
		apiKey, err := assistantPromptSecret(stdin, reader, stdout, apiPrompt, defaultKey)
		if err != nil {
			return cfg, err
		}

		next := cfg
		next.Provider = provider
		next.OllamaURL = strings.TrimSpace(baseURL)
		next.Model = strings.TrimSpace(model)
		next.OllamaAPIKey = strings.TrimSpace(apiKey)
		next.OpenAIKey = ""
		next.AnthropicKey = ""

		if err := SaveAssistantConfigFile(next); err != nil {
			return cfg, err
		}

		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("testing provider connection...")); err != nil {
			return cfg, err
		}
		providerClient, err := NewModelProvider(next)
		if err != nil {
			if _, werr := fmt.Fprintln(stdout, "  "+ui.tyellow(err.Error())); werr != nil {
				return cfg, werr
			}
			if _, err := fmt.Fprintln(stdout); err != nil {
				return cfg, err
			}
			continue
		}
		ok, checkErr := providerClient.IsAvailable()
		if checkErr != nil || !ok {
			msg := "provider check failed"
			if checkErr != nil {
				msg = checkErr.Error()
			}
			if _, err := fmt.Fprintln(stdout, "  "+ui.tyellow(msg)); err != nil {
				return cfg, err
			}
			if _, err := fmt.Fprintln(stdout); err != nil {
				return cfg, err
			}
			continue
		}
		testReply, err := providerClient.Chat([]Message{{Role: "user", Content: "Reply with READY only."}}, nil)
		if err != nil {
			if _, err2 := fmt.Fprintln(stdout, "  "+ui.tyellow("provider test message failed: "+err.Error())); err2 != nil {
				return cfg, err2
			}
			if _, err := fmt.Fprintln(stdout); err != nil {
				return cfg, err
			}
			continue
		}
		if _, err := fmt.Fprintln(stdout, ui.success("provider ready")); err != nil {
			return cfg, err
		}
		if reply := strings.TrimSpace(testReply); reply != "" {
			if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("test reply: "+assistantTruncateText(reply, 80))); err != nil {
				return cfg, err
			}
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return cfg, err
		}
		return next, nil
	}
}

func assistantOnboardGmail(stdin io.Reader, stdout io.Writer, cfg AssistantConfig) (AssistantConfig, error) {
	ui := newTermUI(stdout)
	reader := bufio.NewReader(stdin)

	if _, err := fmt.Fprintln(stdout, ui.tbold("  2. gmail")); err != nil {
		return cfg, err
	}
	gmail, err := NewGmailCapability(cfg)
	if err != nil {
		return cfg, err
	}
	if result, execErr := gmail.Execute("gmail.status", map[string]any{}); execErr == nil && result.Success {
		if data, ok := result.Data.(map[string]any); ok && assistantBoolValue(data["connected"]) {
			email := assistantStringValue(data["email"])
			if _, err := fmt.Fprintln(stdout, ui.success("gmail connected")); err != nil {
				return cfg, err
			}
			if email != "" {
				if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("connected as "+email)); err != nil {
					return cfg, err
				}
			}
			if _, err := fmt.Fprintln(stdout); err != nil {
				return cfg, err
			}
			return cfg, nil
		}
	}

	if !assistantGmailCredentialsExist(cfg) {
		if _, err := gmail.loadOrCreateCredentials(); err == nil {
			if _, err := fmt.Fprintln(stdout, ui.success("gmail credentials found")); err != nil {
				return cfg, err
			}
		} else {
			if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("enter your Google Desktop OAuth client credentials for Gmail.")); err != nil {
				return cfg, err
			}
			clientID, err := assistantPromptLine(reader, stdout, "gmail client id", "")
			if err != nil {
				return cfg, err
			}
			clientSecret, err := assistantPromptSecret(stdin, reader, stdout, "gmail client secret", "")
			if err != nil {
				return cfg, err
			}
			creds := &gmailOAuthCredentials{
				ClientID:     strings.TrimSpace(clientID),
				ClientSecret: strings.TrimSpace(clientSecret),
				TokenURL:     gmailTokenURL,
				Scopes:       append([]string(nil), gmailRequiredScopes...),
			}
			if creds.ClientID == "" {
				return cfg, errors.New("gmail client id is required")
			}
			if err := gmailSaveOAuthCredentials(cfg.GmailCredPath, creds); err != nil {
				return cfg, err
			}
			if _, err := fmt.Fprintln(stdout, ui.success("gmail credentials saved")); err != nil {
				return cfg, err
			}
		}
	}

	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("next, Gmail sign-in will open in your browser and return here when complete.")); err != nil {
		return cfg, err
	}
	if err := gmailAuth(stdout, cfg); err != nil {
		return cfg, err
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func assistantOnboardBrowser(stdin io.Reader, stdout io.Writer, cfg AssistantConfig) (AssistantConfig, error) {
	ui := newTermUI(stdout)
	reader := bufio.NewReader(stdin)

	if _, err := fmt.Fprintln(stdout, ui.tbold("  3. browser computer")); err != nil {
		return cfg, err
	}
	if cfg.BrowserOnboarded {
		if cfg.BrowserEnabled && assistantBrowserProfileExists(cfg) {
			if _, err := fmt.Fprintln(stdout, ui.success("browser computer connected")); err != nil {
				return cfg, err
			}
			if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("profile: "+cfg.BrowserProfilePath)); err != nil {
				return cfg, err
			}
			if _, err := fmt.Fprintln(stdout); err != nil {
				return cfg, err
			}
			return cfg, nil
		}
		if !cfg.BrowserEnabled {
			if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("browser computer skipped")); err != nil {
				return cfg, err
			}
			if _, err := fmt.Fprintln(stdout); err != nil {
				return cfg, err
			}
			return cfg, nil
		}
	}

	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("Jot can use a dedicated local browser profile to browse sites, fill forms, and act in your signed-in web session.")); err != nil {
		return cfg, err
	}
	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("Jot will never submit irreversible browser actions without your approval.")); err != nil {
		return cfg, err
	}
	enable, err := assistantPromptYesNo(reader, stdout, "enable browser computer", true)
	if err != nil {
		return cfg, err
	}
	if !enable {
		cfg.BrowserOnboarded = true
		cfg.BrowserEnabled = false
		cfg.BrowserConnected = false
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("browser computer skipped for now.")); err != nil {
			return cfg, err
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return cfg, err
		}
		return cfg, nil
	}

	next, err := assistantConnectBrowserProfile(stdin, stdout, cfg)
	if err != nil {
		return cfg, err
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return cfg, err
	}
	return next, nil
}

func assistantOnboardChannels(stdin io.Reader, stdout io.Writer, cfg AssistantConfig) (AssistantConfig, error) {
	ui := newTermUI(stdout)
	reader := bufio.NewReader(stdin)

	if _, err := fmt.Fprintln(stdout, ui.tbold("  4. messaging channels")); err != nil {
		return cfg, err
	}
	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("optional: connect WhatsApp, Telegram, Discord, or Instagram so Jot can read and reply from your signed-in sessions.")); err != nil {
		return cfg, err
	}
	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("Jot will keep the sessions local and escalate anything important back to you.")); err != nil {
		return cfg, err
	}
	defaultValue := strings.Join(assistantConnectedChannelNames(cfg), ",")
	value, err := assistantPromptLine(reader, stdout, "channels to connect now (comma-separated, blank to skip)", defaultValue)
	if err != nil {
		return cfg, err
	}
	names := assistantParseChannelSelection(value)
	if len(names) == 0 {
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("messaging channels skipped for now.")); err != nil {
			return cfg, err
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return cfg, err
		}
		return cfg, nil
	}
	next := cfg
	for _, channel := range names {
		var connectErr error
		next, connectErr = assistantConnectMessagingChannel(stdin, stdout, next, channel)
		if connectErr != nil {
			return cfg, connectErr
		}
		if err := SaveAssistantConfigFile(next); err != nil {
			return cfg, err
		}
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return cfg, err
	}
	return next, nil
}

func assistantPromptLine(reader *bufio.Reader, stdout io.Writer, label, defaultValue string) (string, error) {
	prompt := "  " + strings.TrimSpace(label)
	if strings.TrimSpace(defaultValue) != "" {
		prompt += " [" + strings.TrimSpace(defaultValue) + "]"
	}
	prompt += ": "
	if _, err := fmt.Fprint(stdout, prompt); err != nil {
		return "", err
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	value := strings.TrimSpace(line)
	if value == "" {
		value = strings.TrimSpace(defaultValue)
	}
	return value, nil
}

func assistantPromptYesNo(reader *bufio.Reader, stdout io.Writer, label string, defaultYes bool) (bool, error) {
	defaultValue := "y"
	if !defaultYes {
		defaultValue = "n"
	}
	value, err := assistantPromptLine(reader, stdout, label+" [y/n]", defaultValue)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "y", "yes":
		if strings.TrimSpace(value) == "" {
			return defaultYes, nil
		}
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return defaultYes, nil
	}
}

func assistantPromptSecret(stdin io.Reader, reader *bufio.Reader, stdout io.Writer, label, defaultValue string) (string, error) {
	prompt := "  " + strings.TrimSpace(label)
	if strings.TrimSpace(defaultValue) != "" {
		prompt += " [press enter to keep current]"
	}
	prompt += ": "
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		if _, err := fmt.Fprint(stdout, prompt); err != nil {
			return "", err
		}
		data, err := term.ReadPassword(int(file.Fd()))
		if _, werr := fmt.Fprintln(stdout); werr != nil && err == nil {
			err = werr
		}
		if err != nil {
			return "", err
		}
		value := strings.TrimSpace(string(data))
		if value == "" {
			value = strings.TrimSpace(defaultValue)
		}
		return value, nil
	}
	return assistantPromptLine(reader, stdout, label, defaultValue)
}

func assistantDefaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return fallback
}

func assistantOnboardingDefaultProvider(cfg AssistantConfig) string {
	if strings.EqualFold(strings.TrimSpace(cfg.Provider), "ollama") {
		return "ollama"
	}
	return "ollama"
}

func newAssistantSession(inv assistantInvocation) (*AssistantSession, error) {
	provider, err := NewModelProvider(inv.Config)
	if err != nil {
		return nil, err
	}
	caps, err := buildAssistantCapabilities(inv.Config, inv.Scope, provider)
	if err != nil {
		return nil, err
	}
	session := NewAssistantSession(provider, caps, inv.Config)
	session.Format = inv.Format
	session.Verbose = inv.Verbose
	session.NoConfirm = inv.NoConfirm
	return session, nil
}

func buildAssistantCapabilities(cfg AssistantConfig, scope string, provider ModelProvider) ([]Capability, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	var caps []Capability
	addBackup := scope == "" || scope == "backup"
	addGmail := scope == "" || scope == "gmail"
	addCalendar := scope == "" || scope == "calendar"
	addSetup := scope == "" || scope == "setup"
	var gmail *GmailCapability

	if addSetup {
		caps = append(caps, &SetupCapability{Config: cfg})
	}
	if addBackup {
		caps = append(caps, &BackupCapability{Config: cfg})
	}
	if addGmail {
		var err error
		gmail, err = NewGmailCapability(cfg)
		if err != nil {
			return nil, err
		}
		caps = append(caps, gmail)
	}
	if addCalendar {
		caps = append(caps, &CalendarCapability{})
	}
	if scope == "fs" {
		return nil, errors.New("filesystem capability is not implemented in v1")
	}
	if scope == "" || scope == "whatsapp" {
		if channel := assistantChannelConfig(cfg, assistantChannelWhatsApp); channel.Enabled && channel.Connected {
			if assistantChannelHasNativeBridge(cfg, assistantChannelWhatsApp) {
				whatsapp, err := NewWhatsAppCapability(cfg, provider, gmail)
				if err != nil {
					if scope == "whatsapp" {
						return nil, err
					}
				} else {
					caps = append(caps, whatsapp)
				}
			} else if scope == "whatsapp" {
				return nil, fmt.Errorf("WhatsApp is connected but no native bridge is configured")
			}
		}
	}
	if len(caps) == 0 {
		return nil, fmt.Errorf("unknown capability scope %q", scope)
	}
	return caps, nil
}

func runAssistantInteractive(stdin io.Reader, stdout io.Writer, session *AssistantSession, inv assistantInvocation, now func() time.Time) error {
	return RunAssistantInteractive(context.Background(), session, stdin, stdout, now)
}

func runAssistantSingleShot(stdin io.Reader, stdout io.Writer, session *AssistantSession, inv assistantInvocation, now func() time.Time) error {
	result, err := RunAssistantTurn(context.Background(), session, inv.Prompt, stdin, stdout, now)
	if err != nil {
		return err
	}
	if now == nil {
		now = time.Now
	}
	rendered, err := RenderAssistantTurn(inv.Prompt, result, session.Provider, inv.Format, now())
	if err != nil {
		return err
	}
	if result != nil && result.StreamedFinal {
		if _, err := fmt.Fprintln(stdout); err != nil {
			return err
		}
	}
	if rendered == "" {
		return nil
	}
	_, err = fmt.Fprintln(stdout, rendered)
	return err
}

func runAssistantStatus(stdout io.Writer, inv assistantInvocation) error {
	provider, providerErr := NewModelProvider(inv.Config)
	available := false
	if providerErr == nil && provider != nil {
		ok, err := provider.IsAvailable()
		available = ok && err == nil
	}

	status := assistantStatusSnapshot{
		Provider:          inv.Config.Provider,
		Model:             inv.Config.Model,
		ProviderAvailable: available,
		Format:            inv.Format,
		Verbose:           inv.Verbose,
		Scope:             inv.Scope,
		BrowserEnabled:    inv.Config.BrowserEnabled,
		BrowserConnected:  inv.Config.BrowserEnabled && inv.Config.BrowserConnected && assistantBrowserProfileExists(inv.Config),
		BrowserProfile:    strings.TrimSpace(inv.Config.BrowserProfilePath),
		ConnectedChannels: assistantConnectedChannelNames(inv.Config),
	}

	gmail, err := NewGmailCapability(inv.Config)
	if err == nil {
		if result, execErr := gmail.Execute("gmail.status", map[string]any{}); execErr == nil && result.Success {
			if data, ok := result.Data.(map[string]any); ok {
				status.GmailConnected = assistantBoolValue(data["connected"])
				status.GmailEmail = assistantStringValue(data["email"])
				status.GmailSendReady = assistantBoolValue(data["sendReady"])
			}
		}
	}

	if inv.Format == "json" {
		return writeJSON(stdout, status)
	}

	ui := newTermUI(stdout)
	if _, err := fmt.Fprint(stdout, ui.header("Assistant Status")); err != nil {
		return err
	}
	providerLine := fmt.Sprintf("provider: %s", status.Provider)
	if status.ProviderAvailable {
		if _, err := fmt.Fprintln(stdout, ui.success(providerLine)); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(stdout, "  "+ui.tyellow(providerLine)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("model: "+status.Model)); err != nil {
		return err
	}
	if status.GmailConnected {
		suffix := ""
		if status.GmailSendReady {
			suffix = ", send ready"
		} else {
			suffix = ", send not granted"
		}
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("gmail: connected ("+status.GmailEmail+suffix+")")); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("gmail: disconnected")); err != nil {
			return err
		}
	}
	switch {
	case status.BrowserConnected:
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("browser computer: connected")); err != nil {
			return err
		}
	case status.BrowserEnabled:
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("browser computer: sign-in needed")); err != nil {
			return err
		}
	default:
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("browser computer: disabled")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("format: "+inv.Format)); err != nil {
		return err
	}
	if len(status.ConnectedChannels) > 0 {
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("channels: "+strings.Join(status.ConnectedChannels, ", "))); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("channels: none connected")); err != nil {
			return err
		}
	}
	return nil
}

func runAssistantWelcomeSummary(stdout io.Writer, session *AssistantSession, now func() time.Time) error {
	if session == nil {
		return nil
	}
	if now == nil {
		now = time.Now
	}
	gmail := assistantSessionGmailCapability(session)
	if gmail == nil {
		return nil
	}

	result, err := gmail.Execute("gmail.search", map[string]any{
		"query": "in:anywhere",
		"max":   10,
	})
	if err != nil {
		return err
	}
	emails, _ := result.Data.([]NormalizedEmail)
	if len(emails) == 0 {
		return nil
	}

	semantic, semErr := assistantSummarizeEmailsSemantically(session.Provider, "recent inbox messages", emails, now())
	if semErr != nil {
		semantic = assistantFallbackSemanticSummary("recent inbox messages", emails)
	}
	threads, _ := assistantViewsFromEmails(emails, now())
	renderer := NewAssistantConsoleRenderer(stdout, session.Format, false)
	ui := newTermUI(stdout)
	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("here are your 10 most recent emails to get you started.")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return err
	}
	if err := renderer.RenderThreads(fmt.Sprintf("Gmail · %d recent messages", len(threads)), threads); err != nil {
		return err
	}
	if strings.TrimSpace(semantic.Summary) != "" {
		if _, err := fmt.Fprintln(stdout); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(stdout, strings.TrimSpace(semantic.Summary)); err != nil {
			return err
		}
	}
	if actions := assistantSemanticActionsToViews(semantic.Actions); len(actions) > 0 {
		if err := renderer.RenderActions("getting started:", actions); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return err
	}
	return nil
}

func assistantSessionGmailCapability(session *AssistantSession) *GmailCapability {
	if session == nil {
		return nil
	}
	for _, cap := range session.Capabilities {
		if gmail, ok := cap.(*GmailCapability); ok {
			return gmail
		}
	}
	return nil
}

func runAssistantAuth(stdin io.Reader, stdout io.Writer, inv assistantInvocation) error {
	if len(inv.Args) == 0 {
		return fmt.Errorf("usage: jot assistant auth <gmail|browser|whatsapp|telegram|discord|instagram>")
	}
	name := assistantNormalizeChannelName(inv.Args[0])
	switch strings.ToLower(strings.TrimSpace(inv.Args[0])) {
	case "gmail":
		return gmailAuth(stdout, inv.Config)
	case "browser":
		cfg, err := assistantConnectBrowserProfile(stdin, stdout, inv.Config)
		if err != nil {
			return err
		}
		return SaveAssistantConfigFile(cfg)
	default:
		if name != "" {
			cfg, err := assistantConnectMessagingChannel(stdin, stdout, inv.Config, name)
			if err != nil {
				return err
			}
			return SaveAssistantConfigFile(cfg)
		}
		return fmt.Errorf("usage: jot assistant auth <gmail|browser|whatsapp|telegram|discord|instagram>")
	}
}

func runAssistantChannels(stdin io.Reader, stdout io.Writer, inv assistantInvocation) error {
	if len(inv.Args) == 0 {
		return fmt.Errorf("usage: jot assistant channels <status|connect|disconnect> [channel]")
	}
	switch strings.ToLower(strings.TrimSpace(inv.Args[0])) {
	case "status":
		return runAssistantChannelsStatus(stdout, inv.Config, inv.Format)
	case "connect":
		if len(inv.Args) < 2 {
			return fmt.Errorf("usage: jot assistant channels connect <whatsapp|telegram|discord|instagram>")
		}
		cfg, err := assistantConnectMessagingChannel(stdin, stdout, inv.Config, inv.Args[1])
		if err != nil {
			return err
		}
		return SaveAssistantConfigFile(cfg)
	case "disconnect":
		if len(inv.Args) < 2 {
			return fmt.Errorf("usage: jot assistant channels disconnect <whatsapp|telegram|discord|instagram>")
		}
		cfg, err := assistantDisconnectMessagingChannel(stdout, inv.Config, inv.Args[1])
		if err != nil {
			return err
		}
		return SaveAssistantConfigFile(cfg)
	default:
		return fmt.Errorf("usage: jot assistant channels <status|connect|disconnect> [channel]")
	}
}

func runAssistantBrowser(stdin io.Reader, stdout io.Writer, inv assistantInvocation) error {
	if len(inv.Args) == 0 {
		return fmt.Errorf("usage: jot assistant browser <connect|status|disconnect>")
	}
	switch strings.ToLower(strings.TrimSpace(inv.Args[0])) {
	case "connect":
		cfg, err := assistantConnectBrowserProfile(stdin, stdout, inv.Config)
		if err != nil {
			return err
		}
		return SaveAssistantConfigFile(cfg)
	case "status":
		return runAssistantBrowserStatus(stdout, inv.Config, inv.Format)
	case "disconnect":
		cfg, err := assistantDisconnectBrowserProfile(stdout, inv.Config)
		if err != nil {
			return err
		}
		return SaveAssistantConfigFile(cfg)
	default:
		return fmt.Errorf("usage: jot assistant browser <connect|status|disconnect>")
	}
}

func assistantConnectBrowserProfile(stdin io.Reader, stdout io.Writer, cfg AssistantConfig) (AssistantConfig, error) {
	ui := newTermUI(stdout)
	reader := bufio.NewReader(stdin)

	profilePath := strings.TrimSpace(cfg.BrowserProfilePath)
	if profilePath == "" {
		defaultPath, err := defaultBrowserProfileDir()
		if err != nil {
			return cfg, err
		}
		profilePath = defaultPath
	}
	next := cfg
	next.BrowserProfilePath = profilePath

	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("opening the dedicated Jot browser profile...")); err != nil {
		return cfg, err
	}
	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("sign into Google in that window, then return here and press enter.")); err != nil {
		return cfg, err
	}
	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("this lets Jot help with authenticated browser tasks like Google Forms while keeping the session local to your machine.")); err != nil {
		return cfg, err
	}

	browser, err := NewBrowserComputer(BrowserComputerOptions{
		UserDataDir: profilePath,
		StartURL:    assistantBrowserConnectURL(),
		Visible:     true,
	})
	if err != nil {
		return cfg, err
	}
	defer browser.Close()

	if _, err := assistantPromptLine(reader, stdout, "press enter when browser sign-in is complete", ""); err != nil {
		return cfg, err
	}

	if err := browser.Open(assistantBrowserVerifyURL()); err != nil {
		return cfg, err
	}
	time.Sleep(2 * time.Second)
	snapshot, err := browser.Snapshot()
	if err != nil {
		return cfg, err
	}
	if !assistantBrowserLooksSignedIn(snapshot) {
		return cfg, errors.New("browser computer sign-in could not be confirmed; open `jot assistant browser connect` and try again")
	}

	next.BrowserEnabled = true
	next.BrowserOnboarded = true
	next.BrowserConnected = true
	if _, err := fmt.Fprintln(stdout, ui.success("browser computer connected")); err != nil {
		return cfg, err
	}
	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("profile: "+profilePath)); err != nil {
		return cfg, err
	}
	return next, nil
}

func runAssistantBrowserStatus(stdout io.Writer, cfg AssistantConfig, format string) error {
	status := map[string]any{
		"enabled":    cfg.BrowserEnabled,
		"connected":  cfg.BrowserEnabled && cfg.BrowserConnected && assistantBrowserProfileExists(cfg),
		"profile":    strings.TrimSpace(cfg.BrowserProfilePath),
		"onboarded":  cfg.BrowserOnboarded,
		"profileDir": assistantBrowserProfileExists(cfg),
	}
	if format == "json" {
		return writeJSON(stdout, status)
	}
	ui := newTermUI(stdout)
	if _, err := fmt.Fprint(stdout, ui.header("Browser Computer")); err != nil {
		return err
	}
	switch {
	case assistantBoolValue(status["connected"]):
		if _, err := fmt.Fprintln(stdout, ui.success("browser computer connected")); err != nil {
			return err
		}
	case cfg.BrowserEnabled:
		if _, err := fmt.Fprintln(stdout, "  "+ui.tyellow("browser computer enabled but sign-in is not confirmed")); err != nil {
			return err
		}
	default:
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("browser computer disabled")); err != nil {
			return err
		}
	}
	if path := strings.TrimSpace(cfg.BrowserProfilePath); path != "" {
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("profile: "+path)); err != nil {
			return err
		}
	}
	return nil
}

func assistantDisconnectBrowserProfile(stdout io.Writer, cfg AssistantConfig) (AssistantConfig, error) {
	next := cfg
	path := strings.TrimSpace(next.BrowserProfilePath)
	if path != "" {
		cleanPath := filepath.Clean(path)
		if !strings.Contains(strings.ToLower(cleanPath), "jot") || !strings.Contains(strings.ToLower(cleanPath), "browser-profile") {
			return cfg, fmt.Errorf("refusing to remove unexpected browser profile path %q", path)
		}
		if err := os.RemoveAll(path); err != nil {
			return cfg, err
		}
	}
	next.BrowserEnabled = false
	next.BrowserOnboarded = true
	next.BrowserConnected = false
	ui := newTermUI(stdout)
	if _, err := fmt.Fprintln(stdout, ui.success("browser computer disconnected")); err != nil {
		return cfg, err
	}
	if path != "" {
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("removed browser profile at "+path)); err != nil {
			return cfg, err
		}
	}
	return next, nil
}

func assistantConnectMessagingChannel(stdin io.Reader, stdout io.Writer, cfg AssistantConfig, channel string) (AssistantConfig, error) {
	channel = assistantNormalizeChannelName(channel)
	if channel == "" {
		return cfg, fmt.Errorf("unknown channel")
	}
	ui := newTermUI(stdout)
	if channel == assistantChannelWhatsApp {
		return assistantConnectWhatsAppChannel(stdout, cfg, ui)
	}
	reader := bufio.NewReader(stdin)
	next := cfg
	settings := assistantChannelConfig(next, channel)

	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("opening "+assistantChannelDisplayName(channel)+" in a dedicated Jot browser profile...")); err != nil {
		return cfg, err
	}
	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("sign in yourself in that browser window, then return here and press enter.")); err != nil {
		return cfg, err
	}
	if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("Jot keeps the session local and will only send messages with your approval.")); err != nil {
		return cfg, err
	}

	browser, err := NewBrowserComputer(BrowserComputerOptions{
		UserDataDir: settings.BrowserProfilePath,
		StartURL:    assistantChannelConnectURL(channel),
		Visible:     true,
	})
	if err != nil {
		return cfg, err
	}
	defer browser.Close()

	if _, err := assistantPromptLine(reader, stdout, "press enter when "+assistantChannelDisplayName(channel)+" sign-in is complete", ""); err != nil {
		return cfg, err
	}
	if verifyURL := strings.TrimSpace(assistantChannelVerifyURL(channel)); verifyURL != "" {
		if err := browser.Open(verifyURL); err != nil {
			return cfg, err
		}
	}
	time.Sleep(2 * time.Second)
	snapshot, err := browser.Snapshot()
	if err != nil {
		return cfg, err
	}
	if !assistantChannelLooksConnected(channel, snapshot) {
		return cfg, fmt.Errorf("%s sign-in could not be confirmed; run `jot assistant channels connect %s` and try again", assistantChannelDisplayName(channel), channel)
	}
	settings.Enabled = true
	settings.Onboarded = true
	settings.Connected = true
	settings.AccountLabel = assistantDefaultString(assistantChannelAccountLabel(channel, snapshot), settings.AccountLabel)
	if next.Channels == nil {
		next.Channels = make(map[string]AssistantChannelConfig)
	}
	next.Channels[channel] = settings
	if _, err := fmt.Fprintln(stdout, ui.success(assistantChannelDisplayName(channel)+" connected")); err != nil {
		return cfg, err
	}
	if settings.AccountLabel != "" {
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("account: "+settings.AccountLabel)); err != nil {
			return cfg, err
		}
	}
	return next, nil
}

func assistantConnectWhatsAppChannel(stdout io.Writer, cfg AssistantConfig, ui termUI) (AssistantConfig, error) {
	next, err := assistantMaybePrepareWhatsAppBridge(stdout, cfg)
	if err != nil {
		return cfg, err
	}
	settings := assistantChannelConfig(next, assistantChannelWhatsApp)
	settings.Enabled = true
	settings.Onboarded = true
	settings.Connected = false
	if next.Channels == nil {
		next.Channels = make(map[string]AssistantChannelConfig)
	}
	if strings.TrimSpace(settings.BridgeCommand) == "" {
		next.Channels[assistantChannelWhatsApp] = settings
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("WhatsApp now uses a native local bridge instead of browser DOM automation.")); err != nil {
			return cfg, err
		}
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("Configure `channels.whatsapp.bridgeCommand` and optional `bridgeArgs` in assistant.json to point at your Baileys-style bridge.")); err != nil {
			return cfg, err
		}
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("Then run `jot assistant channels connect whatsapp` again to verify the connection.")); err != nil {
			return cfg, err
		}
		return next, nil
	}
	adapter, err := newAssistantChannelAdapter(next, assistantChannelWhatsApp)
	if err != nil {
		return cfg, err
	}
	status, err := adapter.Status(context.Background())
	if err != nil {
		return cfg, err
	}
	settings.Connected = status.Connected
	settings.AccountLabel = assistantDefaultString(status.AccountLabel, settings.AccountLabel)
	next.Channels[assistantChannelWhatsApp] = settings
	if status.Connected {
		if _, err := fmt.Fprintln(stdout, ui.success("WhatsApp connected")); err != nil {
			return cfg, err
		}
		if settings.AccountLabel != "" {
			if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("account: "+settings.AccountLabel)); err != nil {
				return cfg, err
			}
		}
		return next, nil
	}
	if _, err := fmt.Fprintln(stdout, "  "+ui.tyellow("WhatsApp bridge is configured but not connected yet.")); err != nil {
		return cfg, err
	}
	if detail := strings.TrimSpace(status.Detail); detail != "" {
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim(detail)); err != nil {
			return cfg, err
		}
	}
	return next, nil
}

func assistantDisconnectMessagingChannel(stdout io.Writer, cfg AssistantConfig, channel string) (AssistantConfig, error) {
	channel = assistantNormalizeChannelName(channel)
	if channel == "" {
		return cfg, fmt.Errorf("unknown channel")
	}
	next := cfg
	settings := assistantChannelConfig(next, channel)
	path := strings.TrimSpace(settings.BrowserProfilePath)
	if path != "" {
		cleanPath := filepath.Clean(path)
		if !strings.Contains(strings.ToLower(cleanPath), "jot") || !strings.Contains(strings.ToLower(cleanPath), channel) {
			return cfg, fmt.Errorf("refusing to remove unexpected channel profile path %q", path)
		}
		if err := os.RemoveAll(path); err != nil {
			return cfg, err
		}
	}
	settings.Enabled = false
	settings.Onboarded = true
	settings.Connected = false
	settings.AccountLabel = ""
	settings.LastCursor = ""
	if next.Channels == nil {
		next.Channels = make(map[string]AssistantChannelConfig)
	}
	next.Channels[channel] = settings
	ui := newTermUI(stdout)
	if _, err := fmt.Fprintln(stdout, ui.success(assistantChannelDisplayName(channel)+" disconnected")); err != nil {
		return cfg, err
	}
	return next, nil
}

func runAssistantChannelsStatus(stdout io.Writer, cfg AssistantConfig, format string) error {
	status := assistantChannelStatusMap(cfg)
	if format == "json" {
		return writeJSON(stdout, status)
	}
	ui := newTermUI(stdout)
	if _, err := fmt.Fprint(stdout, ui.header("Messaging Channels")); err != nil {
		return err
	}
	for _, channel := range assistantSupportedChannels() {
		item := status[channel]
		state := AssistantChannelStatusForConfig(assistantChannelConfig(cfg, channel)).State
		if state == "disabled" {
			state = "not connected"
		}
		if _, err := fmt.Fprintln(stdout, "  "+ui.tdim(assistantChannelDisplayName(channel)+": "+state)); err != nil {
			return err
		}
		if label := strings.TrimSpace(assistantStringValue(item["account"])); label != "" {
			if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("  account: "+label)); err != nil {
				return err
			}
		}
		if bridge := strings.TrimSpace(assistantStringValue(item["bridge"])); bridge != "" {
			if _, err := fmt.Fprintln(stdout, "  "+ui.tdim("  bridge: "+bridge)); err != nil {
				return err
			}
		}
	}
	return nil
}

func assistantBrowserConnectURL() string {
	return "https://accounts.google.com/ServiceLogin?continue=https%3A%2F%2Fmyaccount.google.com%2F&hl=en"
}

func assistantBrowserVerifyURL() string {
	return "https://myaccount.google.com/?hl=en"
}

func assistantBrowserLooksSignedIn(snapshot BrowserPageSnapshot) bool {
	text := strings.ToLower(strings.TrimSpace(snapshot.Title + " " + snapshot.Text))
	if strings.Contains(text, "sign in to continue") ||
		strings.Contains(text, "to continue to") ||
		strings.Contains(text, "choose an account") ||
		strings.Contains(text, "use your google account") {
		return false
	}
	if strings.Contains(text, "google account") ||
		strings.Contains(text, "personal info") ||
		strings.Contains(text, "privacy") ||
		strings.Contains(text, "security") {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(snapshot.URL)), "myaccount.google.com")
}

func runAssistantGmail(stdin io.Reader, stdout io.Writer, inv assistantInvocation, now func() time.Time) error {
	if len(inv.Args) == 0 {
		return fmt.Errorf("usage: jot assistant gmail <status|search|summarize|attachments>")
	}
	gmail, err := NewGmailCapability(inv.Config)
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(inv.Args[0])) {
	case "status":
		result, err := gmail.Execute("gmail.status", map[string]any{})
		return assistantWriteToolResult(stdout, inv.Format, result, err)
	case "search":
		return runAssistantGmailSearch(stdout, inv, gmail)
	case "summarize":
		return runAssistantGmailSummarize(stdout, inv, gmail, now)
	case "attachments":
		return runAssistantGmailAttachments(stdin, stdout, inv, gmail, now)
	default:
		return fmt.Errorf("unknown gmail subcommand %q", inv.Args[0])
	}
}

func runAssistantGmailSearch(stdout io.Writer, inv assistantInvocation, gmail *GmailCapability) error {
	query := strings.TrimSpace(strings.Join(inv.Args[1:], " "))
	if query == "" {
		query = mapNLToGmailQuery(inv.Prompt)
	}
	result, err := gmail.Execute("gmail.search", map[string]any{"query": query, "max": 10})
	if err != nil {
		return err
	}
	emails, _ := result.Data.([]NormalizedEmail)
	threads, _ := assistantViewsFromEmails(emails, time.Now())
	renderer := NewAssistantConsoleRenderer(stdout, inv.Format, inv.Verbose)
	if inv.Format == "json" {
		return renderer.WriteJSON(map[string]any{
			"query":   query,
			"threads": threads,
		})
	}
	if err := renderer.RenderThreads(fmt.Sprintf("Gmail · %d messages", len(threads)), threads); err != nil {
		return err
	}
	return nil
}

func runAssistantGmailSummarize(stdout io.Writer, inv assistantInvocation, gmail *GmailCapability, now func() time.Time) error {
	query := mapGmailSummaryQuery(inv.Args[1:], inv.Prompt)
	result, err := gmail.Execute("gmail.search", map[string]any{"query": query, "max": 12})
	if err != nil {
		return err
	}
	emails, _ := result.Data.([]NormalizedEmail)
	threads, _ := assistantViewsFromEmails(emails, now())
	provider, providerErr := NewModelProvider(inv.Config)
	semantic := assistantFallbackSemanticSummary(query, emails)
	if providerErr == nil {
		if triage, err := assistantSummarizeEmailsSemantically(provider, query, emails, now()); err == nil {
			semantic = triage
		}
	}
	actions := assistantSemanticActionsToViews(semantic.Actions)
	renderer := NewAssistantConsoleRenderer(stdout, inv.Format, inv.Verbose)
	if inv.Format == "json" {
		return renderer.WriteJSON(map[string]any{
			"query":   query,
			"threads": threads,
			"summary": semantic.Summary,
			"actions": semantic.Actions,
		})
	}
	if err := renderer.RenderThreads(fmt.Sprintf("Gmail · %d unread messages", len(threads)), threads); err != nil {
		return err
	}
	if semantic.Summary == "" && len(actions) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(stdout, ""); err != nil {
		return err
	}
	return renderer.RenderFinal(semantic.Summary, actions, nil)
}

func runAssistantGmailAttachments(stdin io.Reader, stdout io.Writer, inv assistantInvocation, gmail *GmailCapability, now func() time.Time) error {
	saveDir := ""
	window := "30d"
	for i := 1; i < len(inv.Args); i++ {
		switch inv.Args[i] {
		case "--save":
			if i+1 < len(inv.Args) {
				saveDir = inv.Args[i+1]
				i++
			}
		case "--last":
			if i+1 < len(inv.Args) {
				window = normalizeRelativeWindow(inv.Args[i+1])
				i++
			}
		}
	}
	if saveDir == "" {
		saveDir = inv.Config.AttachmentSaveDir
	}
	query := "has:attachment newer_than:" + window
	result, err := gmail.Execute("gmail.search", map[string]any{"query": query, "max": 25})
	if err != nil {
		return err
	}
	emails, _ := result.Data.([]NormalizedEmail)

	var attachments []AttachmentMeta
	for _, email := range emails {
		attachments = append(attachments, email.Attachments...)
	}
	if len(attachments) == 0 {
		return assistantWriteToolResult(stdout, inv.Format, ToolResult{
			Success: true,
			Data:    []AttachmentMeta{},
			Text:    "no attachments found",
		}, nil)
	}

	if !inv.NoConfirm {
		req := ConfirmationRequest{
			ToolName:    "gmail.download_attachment",
			Description: fmt.Sprintf("download %d attachment(s) to %s?", len(attachments), saveDir),
		}
		if _, err := PromptForConfirmation(stdin, stdout, req); err != nil {
			return err
		}
	}

	var saved []gmailAttachmentDownloadResult
	for _, email := range emails {
		if len(email.Attachments) == 0 {
			continue
		}
		toolResult, err := gmail.Execute("gmail.download_attachment", map[string]any{
			"message_id":   email.ID,
			"download_all": true,
			"save_dir":     saveDir,
		})
		if err != nil {
			return err
		}
		if result, ok := toolResult.Data.(gmailAttachmentDownloadResult); ok {
			saved = append(saved, result)
		}
	}

	renderer := NewAssistantConsoleRenderer(stdout, inv.Format, inv.Verbose)
	if inv.Format == "json" {
		return renderer.WriteJSON(map[string]any{
			"query":       query,
			"attachments": attachments,
			"saved":       saved,
		})
	}
	if _, err := fmt.Fprintf(stdout, "%s\n", newTermUI(stdout).header("Gmail Attachments")); err != nil {
		return err
	}
	for _, item := range saved {
		if len(item.Files) > 0 {
			for _, file := range item.Files {
				if _, err := fmt.Fprintln(stdout, "  "+file.SavedPath); err != nil {
					return err
				}
			}
			continue
		}
		if strings.TrimSpace(item.SavedPath) != "" {
			if _, err := fmt.Fprintln(stdout, "  "+item.SavedPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func runAssistantViewer(stdout io.Writer, session *AssistantSession, inv assistantInvocation, now func() time.Time) error {
	backend := newAssistantViewerBackend(session, now)
	if strings.TrimSpace(inv.Prompt) != "" {
		_, _ = backend.SubmitChat(inv.Prompt)
	}
	return serveAssistantViewer(stdout, backend, 15*time.Minute, now, true)
}

func assistantWriteToolResult(stdout io.Writer, format string, result ToolResult, err error) error {
	if err != nil {
		return err
	}
	if format == "json" {
		return writeJSON(stdout, result)
	}
	if result.Text != "" {
		_, err := fmt.Fprintln(stdout, result.Text)
		return err
	}
	return writeJSON(stdout, result.Data)
}

func assistantViewsFromEmails(emails []NormalizedEmail, now time.Time) ([]AssistantThreadView, []AssistantActionView) {
	_ = now
	threads := make([]AssistantThreadView, 0, len(emails))
	for _, email := range emails {
		threads = append(threads, AssistantThreadView{
			ID:        email.ThreadID,
			Sender:    email.From,
			Subject:   email.Subject,
			Snippet:   email.Snippet,
			Timestamp: email.Date,
			Unread:    email.Unread,
			Messages: []AssistantMessageView{{
				Role:        "assistant",
				Author:      email.From,
				At:          email.Date,
				Content:     email.BodyText,
				Attachments: assistantAttachmentViews(email.Attachments),
			}},
			Attachments: assistantAttachmentViews(email.Attachments),
		})
	}
	return threads, nil
}

func assistantContainsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func assistantSummarizeEmailsSemantically(provider ModelProvider, query string, emails []NormalizedEmail, now time.Time) (AssistantSemanticSummary, error) {
	if len(emails) == 0 {
		return AssistantSemanticSummary{Summary: "No matching emails found."}, nil
	}
	if provider == nil {
		return assistantFallbackSemanticSummary(query, emails), errors.New("assistant provider is nil")
	}

	payload, err := json.MarshalIndent(map[string]any{
		"query":       query,
		"currentTime": now.Format(time.RFC3339),
		"emails":      assistantSemanticEmailInputs(emails),
	}, "", "  ")
	if err != nil {
		return assistantFallbackSemanticSummary(query, emails), err
	}

	messages := []Message{
		{
			Role: "system",
			Content: strings.TrimSpace(`You triage Gmail messages semantically for a user.
Return exactly one JSON object and nothing else.
The JSON schema is:
{
  "summary": "short paragraph, max 3 sentences",
  "actions": [
    {
      "title": "short action label",
      "detail": "what matters and why",
      "emailId": "gmail message id",
      "kind": "security|deadline|meeting|reply|invoice|maintenance|follow_up|info",
      "priority": "high|medium|low"
    }
  ]
}
Rules:
- Decide from the meaning of the email, not by keyword matching.
- Ignore newsletter boilerplate, legal disclaimers, footers, referral copy, and generic article text.
- Only surface actions a human would reasonably take.
- Prefer the most important 0-5 actions.
- For meeting emails, summarize the actual requested next step and time window.
- For replies, deadlines, security alerts, invoices, maintenance notices, and expiring offers, be explicit.
- If nothing needs action, return an empty actions array.`),
		},
		{Role: "user", Content: string(payload)},
	}

	response, err := provider.Chat(messages, nil)
	if err != nil {
		return assistantFallbackSemanticSummary(query, emails), err
	}

	summary, err := assistantParseSemanticSummary(response)
	if err != nil {
		return assistantFallbackSemanticSummary(query, emails), err
	}
	return assistantNormalizeSemanticSummary(summary, emails), nil
}

func assistantSemanticEmailInputs(emails []NormalizedEmail) []map[string]any {
	items := make([]map[string]any, 0, len(emails))
	for _, email := range emails {
		items = append(items, map[string]any{
			"id":       email.ID,
			"threadId": email.ThreadID,
			"from":     email.From,
			"subject":  email.Subject,
			"date":     email.Date.Format(time.RFC3339),
			"labels":   append([]string(nil), email.Labels...),
			"unread":   email.Unread,
			"snippet":  assistantTruncateText(email.Snippet, 220),
			"body":     assistantTruncateText(email.BodyText, 900),
		})
	}
	return items
}

func assistantParseSemanticSummary(text string) (AssistantSemanticSummary, error) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return AssistantSemanticSummary{}, errors.New("semantic summary response was empty")
	}
	var out AssistantSemanticSummary
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out, nil
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return AssistantSemanticSummary{}, errors.New("semantic summary did not contain JSON")
	}
	if obj, err := parseJSONObject(raw[start : end+1]); err == nil {
		data, marshalErr := json.Marshal(obj)
		if marshalErr != nil {
			return AssistantSemanticSummary{}, marshalErr
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return AssistantSemanticSummary{}, err
		}
		return out, nil
	}
	return AssistantSemanticSummary{}, errors.New("semantic summary JSON could not be parsed")
}

func assistantNormalizeSemanticSummary(summary AssistantSemanticSummary, emails []NormalizedEmail) AssistantSemanticSummary {
	summary.Summary = strings.TrimSpace(summary.Summary)
	if summary.Summary == "" {
		summary.Summary = assistantFallbackSemanticSummary("", emails).Summary
	}
	out := AssistantSemanticSummary{Summary: summary.Summary}
	for _, action := range summary.Actions {
		action.Title = strings.TrimSpace(action.Title)
		action.Detail = strings.TrimSpace(action.Detail)
		action.EmailID = strings.TrimSpace(action.EmailID)
		action.Kind = strings.TrimSpace(action.Kind)
		action.Priority = strings.TrimSpace(action.Priority)
		if action.Title == "" && action.Detail == "" {
			continue
		}
		if action.Title == "" {
			action.Title = action.Detail
		}
		if action.Detail == "" {
			action.Detail = action.Title
		}
		out.Actions = append(out.Actions, action)
		if len(out.Actions) >= 5 {
			break
		}
	}
	return out
}

func assistantFallbackSemanticSummary(query string, emails []NormalizedEmail) AssistantSemanticSummary {
	if len(emails) == 0 {
		return AssistantSemanticSummary{Summary: "No matching emails found."}
	}
	summary := fmt.Sprintf("%d matching emails found.", len(emails))
	if assistantContainsAny(strings.ToLower(query), "unread", "today", "newer_than:1d") {
		summary = fmt.Sprintf("%d unread emails from today.", len(emails))
	}
	if top := assistantFallbackTopSubjects(emails, 3); len(top) > 0 {
		summary += " Top messages: " + strings.Join(top, "; ") + "."
	}
	return AssistantSemanticSummary{Summary: summary}
}

func assistantFallbackTopSubjects(emails []NormalizedEmail, limit int) []string {
	if limit <= 0 {
		limit = 3
	}
	var out []string
	for _, email := range emails {
		subject := strings.TrimSpace(email.Subject)
		if subject == "" {
			continue
		}
		out = append(out, assistantSenderName(email.From)+" — "+subject)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func assistantSemanticActionsToViews(actions []AssistantSemanticAction) []AssistantActionView {
	out := make([]AssistantActionView, 0, len(actions))
	for i, action := range actions {
		detail := strings.TrimSpace(action.Detail)
		if detail == "" {
			detail = strings.TrimSpace(action.Title)
		}
		if detail == "" {
			continue
		}
		out = append(out, AssistantActionView{
			ID:           fmt.Sprintf("semantic-action-%d", i),
			Kind:         strings.TrimSpace(action.Kind),
			Title:        strings.TrimSpace(action.Title),
			Detail:       detail,
			Status:       assistantSemanticPriorityLabel(action.Priority),
			ConfirmLabel: "Open",
			Pending:      true,
		})
	}
	return out
}

func assistantAugmentTurnWithSemanticSummary(turn *AssistantTurnView, provider ModelProvider, prompt string, result *AssistantTurnResult, now time.Time) {
	if turn == nil || result == nil {
		return
	}
	for _, execution := range result.Executions {
		emails, ok := execution.Result.Data.([]NormalizedEmail)
		if !ok || len(emails) == 0 {
			continue
		}
		summary, err := assistantSummarizeEmailsSemantically(provider, prompt, emails, now)
		if err != nil {
			summary = assistantFallbackSemanticSummary(prompt, emails)
		}
		if card := assistantSemanticSummaryCard(summary); card.Body != "" || len(card.Buttons) > 0 {
			turn.Cards = append([]AssistantCardView{card}, turn.Cards...)
		}
		return
	}
}

func assistantSemanticSummaryCard(summary AssistantSemanticSummary) AssistantCardView {
	card := AssistantCardView{
		Kind:    "semantic-summary",
		Eyebrow: "Assistant summary",
		Body:    strings.TrimSpace(summary.Summary),
	}
	var details []string
	for i, action := range summary.Actions {
		label := strings.TrimSpace(action.Title)
		if label == "" {
			label = strings.TrimSpace(action.Detail)
		}
		if label == "" {
			continue
		}
		if detail := strings.TrimSpace(action.Detail); detail != "" {
			details = append(details, detail)
		}
		card.Buttons = append(card.Buttons, AssistantInlineButtonView{
			ID:    fmt.Sprintf("semantic-open-%d", i),
			Label: label,
			Tone:  "warn",
		})
		if len(card.Buttons) >= 4 {
			break
		}
	}
	card.Note = strings.Join(details, " ")
	return card
}

func assistantSupplementalFinalTextCard(text string) (AssistantCardView, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return AssistantCardView{}, false
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "download everything to "):
		saveDir := "."
		fragment := text[strings.Index(lower, "download everything to ")+len("download everything to "):]
		if cut := strings.IndexAny(fragment, ",."); cut >= 0 {
			fragment = fragment[:cut]
		}
		if dir := strings.TrimSpace(strings.Trim(fragment, `"'`)); dir != "" {
			saveDir = dir
		}
		return AssistantCardView{
			Kind: "attachment-save",
			Body: "save to " + saveDir + "?",
			Buttons: []AssistantInlineButtonView{
				{ID: "attachment-save", Label: "y save", Tone: "confirm"},
				{ID: "attachment-skip", Label: "n skip"},
			},
		}, true
	case strings.Contains(lower, "reply `all`"),
		strings.Contains(lower, `reply "all"`),
		strings.Contains(lower, "save to ./dir"),
		strings.Contains(lower, "download all"):
		return AssistantCardView{Kind: "note", Body: text}, true
	default:
		return AssistantCardView{}, false
	}
}

func assistantSemanticPriorityLabel(priority string) string {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "high":
		return "high priority"
	case "medium":
		return "medium priority"
	case "low":
		return "low priority"
	default:
		return "triaged"
	}
}

func assistantTruncateText(text string, max int) string {
	text = strings.TrimSpace(normalizeWhitespace(text))
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return strings.TrimSpace(string(runes[:max])) + "..."
}

func assistantAttachmentViews(meta []AttachmentMeta) []AssistantAttachmentView {
	out := make([]AssistantAttachmentView, 0, len(meta))
	for _, item := range meta {
		out = append(out, AssistantAttachmentView{
			Name:         item.Filename,
			MimeType:     item.MimeType,
			SizeBytes:    item.SizeBytes,
			AttachmentID: item.AttachmentID,
		})
	}
	return out
}

func assistantActionViews(prefix string, actions ExtractedActions) []AssistantActionView {
	var out []AssistantActionView
	for i, item := range actions.ActionItems {
		out = append(out, AssistantActionView{
			ID:           fmt.Sprintf("%s-action-%d", prefix, i),
			Kind:         "action",
			Title:        "Action item",
			Detail:       item,
			Status:       "pending",
			ConfirmLabel: "Confirm",
			Pending:      true,
		})
	}
	for i, deadline := range actions.Deadlines {
		detail := deadline.Task
		if !deadline.Due.IsZero() {
			detail += " by " + deadline.Due.Format(time.RFC822)
		} else if deadline.Raw != "" {
			detail += " by " + deadline.Raw
		}
		out = append(out, AssistantActionView{
			ID:           fmt.Sprintf("%s-deadline-%d", prefix, i),
			Kind:         "deadline",
			Title:        "Deadline",
			Detail:       strings.TrimSpace(detail),
			Status:       "pending",
			ConfirmLabel: "Confirm",
			Pending:      true,
		})
	}
	for i, req := range actions.MeetingReqs {
		out = append(out, AssistantActionView{
			ID:           fmt.Sprintf("%s-meeting-%d", prefix, i),
			Kind:         "meeting",
			Title:        "Meeting request",
			Detail:       req.Subject,
			Status:       "pending",
			ConfirmLabel: "Confirm",
			Pending:      true,
		})
	}
	return out
}

type assistantViewerBackendState struct {
	session *AssistantSession
	now     func() time.Time
	mu      sync.Mutex
	page    AssistantPageData
}

func newAssistantViewerBackend(session *AssistantSession, now func() time.Time) *assistantViewerBackendState {
	if now == nil {
		now = time.Now
	}
	return &assistantViewerBackendState{
		session: session,
		now:     now,
		page: AssistantPageData{
			Title:        "jot assistant",
			Subtitle:     "connected to Gmail",
			Intro:        "type a request or pick one below",
			Provider:     session.Provider.Name(),
			Model:        session.Config.Model,
			Format:       session.Format,
			QuickPrompts: defaultAssistantQuickPrompts(),
		},
	}
}

func (b *assistantViewerBackendState) Snapshot() AssistantPageData {
	b.mu.Lock()
	defer b.mu.Unlock()
	page := b.page
	applyAssistantPageDefaults(&page)
	return page
}

func (b *assistantViewerBackendState) SubmitChat(message string) (AssistantPageData, error) {
	b.mu.Lock()
	page := b.page
	b.mu.Unlock()

	result, err := b.session.RunTurn(context.Background(), message, strings.NewReader("n\n"), io.Discard, b.now)
	if err != nil {
		page.Turns = append(page.Turns, AssistantTurnView{
			Prompt: message,
			Cards: []AssistantCardView{{
				Kind: "note",
				Body: err.Error(),
			}},
		})
		b.mu.Lock()
		b.page = page
		b.mu.Unlock()
		return page, nil
	}
	turn := assistantTurnViewFromResult(message, result, b.now())
	page.Turns = append(page.Turns, turn)
	b.mu.Lock()
	b.page = page
	b.mu.Unlock()
	return page, nil
}

func (b *assistantViewerBackendState) ConfirmAction(actionID string) (AssistantPageData, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for turnIndex := range b.page.Turns {
		for cardIndex := range b.page.Turns[turnIndex].Cards {
			card := &b.page.Turns[turnIndex].Cards[cardIndex]
			for _, button := range card.Buttons {
				if button.ID != actionID {
					continue
				}
				card.Success = assistantButtonSuccessText(button.Label)
				card.Buttons = nil
				return b.page, nil
			}
		}
	}
	return b.page, nil
}

func defaultAssistantQuickPrompts() []AssistantQuickPrompt {
	return []AssistantQuickPrompt{
		{Label: "read what matters", Prompt: "summarize my unread emails from today"},
		{Label: "reply queue", Prompt: "what emails still need my reply?"},
		{Label: "calendar availability", Prompt: "am i free Thursday at 3pm?"},
		{Label: "clear promos", Prompt: "archive promo emails from today"},
	}
}

func assistantTurnViewFromResult(prompt string, result *AssistantTurnResult, now time.Time) AssistantTurnView {
	turn := AssistantTurnView{
		Prompt:      strings.TrimSpace(prompt),
		StatusLines: assistantTurnStatusLines(prompt, result),
	}
	if result == nil {
		return turn
	}

	var downloads []gmailAttachmentDownloadResult
	var lastThread *gmailThreadResult
	hasEmailListCard := false
	hasFormCard := false
	for _, execution := range result.Executions {
		switch data := execution.Result.Data.(type) {
		case []NormalizedEmail:
			card := assistantEmailResultCard(execution.Call, data, now)
			if card.Eyebrow != "" || len(card.Rows) > 0 || card.Note != "" {
				turn.Cards = append(turn.Cards, card)
				hasEmailListCard = true
			}
		case gmailThreadResult:
			threadCopy := data
			lastThread = &threadCopy
			card := assistantThreadCard(threadCopy)
			if card.Eyebrow != "" || card.Body != "" {
				turn.Cards = append(turn.Cards, card)
			}
		case []AssistantChannelThread:
			card := assistantChannelListCard(execution.Call, data)
			if card.Eyebrow != "" || len(card.Rows) > 0 {
				turn.Cards = append(turn.Cards, card)
			}
		case AssistantChannelThread:
			card := assistantChannelThreadCard(data)
			if card.Eyebrow != "" || card.Body != "" {
				turn.Cards = append(turn.Cards, card)
			}
		case FormFillResult:
			card := assistantFormResultCard(data)
			if card.Eyebrow != "" || card.Body != "" || card.Note != "" {
				turn.Cards = append(turn.Cards, card)
				hasFormCard = true
			}
		case AssistantJournalBackup:
			card := assistantJournalBackupCard(data)
			if card.Eyebrow != "" || card.Body != "" || card.Note != "" {
				turn.Cards = append(turn.Cards, card)
			}
		case AssistantJournalImport:
			card := assistantJournalImportCard(data)
			if card.Eyebrow != "" || card.Body != "" || card.Note != "" {
				turn.Cards = append(turn.Cards, card)
			}
		case ExtractedActions:
			turn.Cards = append(turn.Cards, assistantExtractedActionCards(data)...)
		case gmailAttachmentDownloadResult:
			downloads = append(downloads, data)
		case map[string]any:
			if card, ok := assistantDraftCard(data, lastThread); ok {
				turn.Cards = append(turn.Cards, card)
				continue
			}
			if card, ok := assistantToolMapCard(execution, data); ok {
				turn.Cards = append(turn.Cards, card)
			}
		}
	}
	if len(downloads) > 0 {
		turn.Cards = append(turn.Cards, assistantDownloadSummaryCard(downloads))
	}
	if text := strings.TrimSpace(result.FinalText); text != "" && !result.StreamedFinal {
		if assistantShouldSuppressFinalTextForCards(text, hasEmailListCard, hasFormCard) {
			// The card already carries the primary UX for this turn.
		} else if len(turn.Cards) == 0 {
			turn.Cards = append(turn.Cards, AssistantCardView{Kind: "note", Body: text})
		} else if card, ok := assistantSupplementalFinalTextCard(text); ok {
			turn.Cards = append(turn.Cards, card)
		} else if assistantShouldRenderFinalTextCard(text) {
			turn.Cards = append(turn.Cards, AssistantCardView{Kind: "note", Body: text})
		}
	}
	return turn
}

func assistantShouldSuppressFinalTextForCards(text string, hasEmailListCard, hasFormCard bool) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	if hasFormCard {
		return true
	}
	if !hasEmailListCard {
		return false
	}
	return assistantLooksLikeEmailCardSummary(text)
}

func assistantLooksLikeEmailCardSummary(text string) bool {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	switch {
	case lower == "":
		return true
	case strings.Contains(trimmed, "\n"):
		return false
	case strings.HasPrefix(lower, "your "),
		strings.HasPrefix(lower, "here is your"),
		strings.HasPrefix(lower, "i found your"),
		strings.HasPrefix(lower, "yes,"),
		strings.HasPrefix(lower, "no,"),
		strings.Contains(trimmed, ":"):
		return false
	case strings.HasPrefix(lower, "here are"),
		strings.HasPrefix(lower, "i found"),
		strings.HasPrefix(lower, "found "),
		strings.HasPrefix(lower, "these are"),
		strings.HasPrefix(lower, "there are"),
		strings.HasPrefix(lower, "i found the following"),
		strings.HasPrefix(lower, "i found 1 "),
		strings.HasPrefix(lower, "i found 2 "),
		strings.HasPrefix(lower, "i found 3 "):
		return true
	case strings.Contains(lower, "gmail results"),
		strings.Contains(lower, "matching emails"),
		strings.Contains(lower, "unread emails"),
		strings.Contains(lower, "emails from"),
		strings.Contains(lower, "threads from"),
		strings.Contains(lower, "messages from"):
		return true
	default:
		return true
	}
}

func assistantShouldRenderFinalTextCard(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if assistantInvitesAttachmentFollowUp(text) {
		return false
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "send it?"),
		strings.Contains(lower, "send this?"),
		strings.Contains(lower, "create this event?"),
		strings.Contains(lower, "reply \"all\""),
		strings.Contains(lower, "reply `all`"):
		return false
	}
	return strings.Contains(text, "\n") || len([]rune(text)) >= 48
}

func assistantTurnStatusLines(prompt string, result *AssistantTurnResult) []string {
	if result == nil {
		return nil
	}
	if result.LiveStatus {
		return nil
	}
	lines := make([]string, 0, len(result.ToolCalls))
	for _, call := range result.ToolCalls {
		if line := assistantStatusLineForToolCall(prompt, call); line != "" {
			lines = append(lines, line)
		}
	}
	return assistantCompactLines(lines)
}

func assistantStatusLineForToolCall(prompt string, call AssistantToolCall) string {
	switch strings.ToLower(strings.TrimSpace(call.Tool)) {
	case "gmail.search":
		query := firstStringParam(call.Params, "query", "input")
		if query == "" {
			query = mapNLToGmailQuery(prompt)
		}
		return assistantSearchStatusLine(query)
	case "gmail.read_thread":
		return "reading thread..."
	case "gmail.read_message":
		return "reading message..."
	case "gmail.read_attachment":
		return "reading attachment contents..."
	case "whatsapp.list_threads":
		return "reading WhatsApp..."
	case "whatsapp.read_thread":
		return "reading WhatsApp thread..."
	case "whatsapp.draft_reply":
		return "drafting WhatsApp reply..."
	case "gmail.fill_form":
		return "opening form, inspecting fields, and gathering answers..."
	case "setup.connect_service":
		service := strings.TrimSpace(firstStringParam(call.Params, "service", "channel", "name"))
		if service == "" {
			return "setting up integration..."
		}
		return "setting up " + assistantChannelDisplayName(service) + "..."
	case "setup.status_service":
		service := strings.TrimSpace(firstStringParam(call.Params, "service", "channel", "name"))
		if service == "" {
			return "checking integration status..."
		}
		return "checking " + assistantChannelDisplayName(service) + " status..."
	case "backup.export_journal":
		return "creating journal backup..."
	case "backup.import_from_gmail":
		return "importing journal backup from Gmail..."
	case "backup.import_journal":
		return "restoring journal backup..."
	case "gmail.extract_actions":
		return "scanning threads for action items..."
	case "gmail.draft_reply":
		return "drafting reply..."
	case "gmail.send_email":
		if paramBool(call.Params, "send", "deliver") {
			return "sending email..."
		}
		return "drafting email..."
	case "gmail.archive_thread":
		return "clearing thread from inbox..."
	case "gmail.mark_read":
		return "marking thread as read..."
	case "gmail.star_thread":
		return "starring thread..."
	case "calendar.free_busy":
		return "checking calendar availability..."
	case "calendar.find_events":
		return "checking your calendar..."
	case "calendar.update_event":
		return "updating calendar event..."
	case "calendar.cancel_event", "calendar.delete_event":
		return "cancelling calendar event..."
	default:
		return ""
	}
}

func assistantSearchStatusLine(query string) string {
	query = strings.TrimSpace(query)
	switch {
	case query == "":
		return "reading Gmail..."
	case strings.Contains(query, "has:attachment"):
		return "searching Gmail - " + query + "..."
	case strings.Contains(query, "is:unread") && strings.Contains(query, "newer_than:1d"):
		return "reading Gmail - searching unread, today..."
	default:
		return "reading Gmail - " + query + "..."
	}
}

func assistantEmailResultCard(call AssistantToolCall, emails []NormalizedEmail, now time.Time) AssistantCardView {
	_ = now
	query := strings.ToLower(strings.TrimSpace(firstStringParam(call.Params, "query", "input")))
	card := AssistantCardView{Kind: "list"}
	switch {
	case strings.Contains(query, "has:attachment"):
		card.Eyebrow = fmt.Sprintf("Gmail · %d attachments found", len(emails))
		card.Rows = assistantAttachmentRows(emails)
	case strings.Contains(query, "is:unread"):
		card.Eyebrow = fmt.Sprintf("Gmail · %d unread", len(emails))
		card.Rows = assistantEmailRows(emails)
	default:
		card.Eyebrow = fmt.Sprintf("Gmail · %d results", len(emails))
		card.Rows = assistantEmailRows(emails)
	}
	return card
}

func assistantEmailRows(emails []NormalizedEmail) []AssistantCardRowView {
	rows := make([]AssistantCardRowView, 0, len(emails))
	for i, email := range emails {
		rows = append(rows, AssistantCardRowView{
			Index:   i + 1,
			Sender:  assistantSenderName(email.From),
			Subject: email.Subject,
			Detail:  email.Snippet,
			Meta:    assistantRelativeMailTime(email.Date),
		})
	}
	return rows
}

func assistantChannelListCard(call AssistantToolCall, threads []AssistantChannelThread) AssistantCardView {
	card := AssistantCardView{Kind: "list"}
	switch strings.ToLower(strings.TrimSpace(call.Tool)) {
	case "whatsapp.list_threads":
		card.Eyebrow = fmt.Sprintf("WhatsApp · %d thread(s)", len(threads))
	default:
		card.Eyebrow = fmt.Sprintf("Messages · %d thread(s)", len(threads))
	}
	rows := make([]AssistantCardRowView, 0, len(threads))
	for i, thread := range threads {
		rows = append(rows, AssistantCardRowView{
			Index:   i + 1,
			Sender:  assistantDefaultString(thread.ContactLabel, thread.ContactID),
			Subject: assistantDefaultString(thread.Snippet, thread.ThreadID),
			Meta:    assistantRelativeMailTime(thread.LastMessageAt),
			Detail:  assistantChannelUnreadDetail(thread),
		})
	}
	card.Rows = rows
	return card
}

func assistantChannelThreadCard(thread AssistantChannelThread) AssistantCardView {
	return AssistantCardView{
		Kind:    "thread",
		Eyebrow: "WhatsApp · " + assistantDefaultString(thread.ContactLabel, "thread"),
		Title:   assistantDefaultString(thread.ContactLabel, thread.ThreadID),
		Body:    assistantChannelThreadTranscript(thread, 8),
		Note:    assistantChannelUnreadDetail(thread),
	}
}

func assistantChannelUnreadDetail(thread AssistantChannelThread) string {
	switch {
	case thread.UnreadCount > 1:
		return fmt.Sprintf("%d unread", thread.UnreadCount)
	case thread.UnreadCount == 1:
		return "1 unread"
	default:
		return ""
	}
}

func assistantAttachmentRows(emails []NormalizedEmail) []AssistantCardRowView {
	rows := make([]AssistantCardRowView, 0, len(emails))
	for i, email := range emails {
		label := email.Subject
		if len(email.Attachments) > 0 {
			label = email.Attachments[0].Filename
			if email.Attachments[0].SizeBytes > 0 {
				label += " · " + assistantFormatKB(email.Attachments[0].SizeBytes)
			}
		}
		rows = append(rows, AssistantCardRowView{
			Index:   i + 1,
			Sender:  assistantSenderName(email.From),
			Subject: label,
			Meta:    assistantRelativeMailTime(email.Date),
		})
	}
	return rows
}

func assistantFormResultCard(result FormFillResult) AssistantCardView {
	title := strings.TrimSpace(result.FormTitle)
	if title == "" {
		title = "form review"
	}
	filled := 0
	blank := 0
	requiredBlank := 0
	for _, field := range result.Fields {
		if strings.TrimSpace(field.Answer) != "" {
			filled++
		} else {
			blank++
			if field.Field.Required {
				requiredBlank++
			}
		}
	}
	eyebrow := "Form · browser review"
	body := "The form is open in the browser window Jot launched. Review or edit it there, then submit when you're happy."
	note := fmt.Sprintf("%d answer(s) filled", filled)
	if blank > 0 {
		note = fmt.Sprintf("%d answer(s) filled · %d left blank", filled, blank)
	}
	if requiredBlank > 0 {
		note += fmt.Sprintf(" · %d required", requiredBlank)
	}
	if len(result.Notes) > 0 {
		note = strings.TrimSpace(result.Notes[len(result.Notes)-1])
	}
	return AssistantCardView{
		Kind:    "note",
		Eyebrow: eyebrow,
		Title:   title,
		Body:    body,
		Note:    note,
	}
}

func assistantJournalBackupCard(result AssistantJournalBackup) AssistantCardView {
	note := fmt.Sprintf("%d entries", result.EntryCount)
	if !result.Oldest.IsZero() && !result.Newest.IsZero() {
		note = fmt.Sprintf("%s to %s · %d entries",
			result.Oldest.Format("2006-01-02"),
			result.Newest.Format("2006-01-02"),
			result.EntryCount)
	}
	return AssistantCardView{
		Kind:    "note",
		Eyebrow: "Backup · journal archive",
		Title:   result.Filename,
		Body:    result.Path,
		Note:    note,
	}
}

func assistantJournalImportCard(result AssistantJournalImport) AssistantCardView {
	note := fmt.Sprintf("%d total entries", result.TotalCount)
	if !result.Oldest.IsZero() && !result.Newest.IsZero() {
		note = fmt.Sprintf("%s to %s · %d total entries",
			result.Oldest.Format("2006-01-02"),
			result.Newest.Format("2006-01-02"),
			result.TotalCount)
	}
	title := fmt.Sprintf("%d imported · %d skipped", result.ImportedCount, result.DuplicateCount)
	if result.ImportedCount == 0 && result.DuplicateCount == 0 {
		title = "journal restored"
	}
	body := strings.TrimSpace(result.ArchivePath)
	if body == "" {
		body = "journal backup restored"
	}
	return AssistantCardView{
		Kind:    "note",
		Eyebrow: "Backup · journal import",
		Title:   title,
		Body:    body,
		Note:    note,
	}
}

func assistantToolMapCard(execution AssistantToolExecution, data map[string]any) (AssistantCardView, bool) {
	tool := strings.ToLower(strings.TrimSpace(execution.Call.Tool))
	switch {
	case tool == "gmail.send_email":
		return assistantGmailSendCard(execution.Result.Text, data)
	case tool == "backup.import_from_gmail":
		if imported, ok := data["import"].(AssistantJournalImport); ok {
			return assistantJournalImportCard(imported), true
		}
		return AssistantCardView{}, false
	case strings.HasPrefix(tool, "gmail."):
		return assistantGmailMutationCard(execution.Result.Text, data)
	case tool == "calendar.free_busy":
		return assistantCalendarFreeBusyCard(execution.Result.Text, data)
	case tool == "calendar.find_events":
		return assistantCalendarFindEventsCard(execution.Result.Text, data)
	case tool == "calendar.create_event", tool == "calendar.update_event", tool == "calendar.cancel_event", tool == "calendar.delete_event":
		return assistantCalendarMutationCard(tool, execution.Result.Text, data)
	default:
		return AssistantCardView{}, false
	}
}

func assistantGmailMutationCard(resultText string, data map[string]any) (AssistantCardView, bool) {
	operation := strings.TrimSpace(assistantStringValue(data["operation"]))
	if operation == "" {
		return AssistantCardView{}, false
	}
	eyebrow := "Clear"
	if operation == "star_thread" {
		eyebrow = "Keep"
	}
	card := AssistantCardView{
		Kind:    "note",
		Eyebrow: eyebrow,
		Success: strings.TrimSpace(resultText),
	}
	if target, ok := data["target"].(gmailLabelMutationTarget); ok {
		card.Note = strings.TrimSpace(gmailLabelMutationTargetLabel(target))
	}
	if card.Success == "" && card.Note == "" {
		return AssistantCardView{}, false
	}
	return card, true
}

func assistantGmailSendCard(resultText string, data map[string]any) (AssistantCardView, bool) {
	to := strings.TrimSpace(assistantStringValue(data["to"]))
	subject := strings.TrimSpace(assistantStringValue(data["subject"]))
	if to == "" && subject == "" && strings.TrimSpace(resultText) == "" {
		return AssistantCardView{}, false
	}
	card := AssistantCardView{
		Kind:    "note",
		Eyebrow: "Email",
		Title:   subject,
		Note:    to,
	}
	if assistantBoolValue(data["sent"]) {
		card.Success = strings.TrimSpace(resultText)
		card.Eyebrow = "Email · sent"
	} else {
		card.Body = strings.TrimSpace(assistantStringValue(data["preview"]))
		card.Eyebrow = "Email · draft ready"
	}
	if count := assistantIntValue(data["attachment_count"]); count > 0 {
		parts := []string{}
		if strings.TrimSpace(card.Note) != "" {
			parts = append(parts, strings.TrimSpace(card.Note))
		}
		parts = append(parts, fmt.Sprintf("%d attachment(s)", count))
		card.Note = strings.Join(parts, " · ")
	}
	return card, true
}

func assistantCalendarFreeBusyCard(resultText string, data map[string]any) (AssistantCardView, bool) {
	body := strings.TrimSpace(resultText)
	if body == "" {
		return AssistantCardView{}, false
	}
	note := ""
	if count := assistantIntValue(data["busyCount"]); count > 0 {
		note = fmt.Sprintf("%d busy block(s) found", count)
	}
	return AssistantCardView{
		Kind:    "note",
		Eyebrow: "Schedule · availability",
		Body:    body,
		Note:    note,
	}, true
}

func assistantCalendarFindEventsCard(resultText string, data map[string]any) (AssistantCardView, bool) {
	rawEvents, ok := data["events"].([]map[string]any)
	if !ok || len(rawEvents) == 0 {
		if generic, ok := data["events"].([]any); ok {
			rows := make([]AssistantCardRowView, 0, len(generic))
			for i, item := range generic {
				event, ok := item.(map[string]any)
				if !ok {
					continue
				}
				rows = append(rows, assistantCalendarEventRow(i+1, event))
			}
			if len(rows) == 0 {
				return AssistantCardView{}, false
			}
			return AssistantCardView{
				Kind:    "list",
				Eyebrow: assistantCalendarEventsEyebrow(len(rows)),
				Rows:    rows,
				Note:    strings.TrimSpace(resultText),
			}, true
		}
		return AssistantCardView{}, false
	}
	rows := make([]AssistantCardRowView, 0, len(rawEvents))
	for i, event := range rawEvents {
		rows = append(rows, assistantCalendarEventRow(i+1, event))
	}
	return AssistantCardView{
		Kind:    "list",
		Eyebrow: assistantCalendarEventsEyebrow(len(rows)),
		Rows:    rows,
		Note:    strings.TrimSpace(resultText),
	}, true
}

func assistantCalendarEventsEyebrow(count int) string {
	if count == 1 {
		return "Schedule · 1 event"
	}
	return fmt.Sprintf("Schedule · %d events", count)
}

func assistantCalendarMutationCard(toolName, resultText string, data map[string]any) (AssistantCardView, bool) {
	eventMap, ok := assistantNestedMap(data["event"])
	if !ok {
		return AssistantCardView{}, false
	}
	eyebrow := "Schedule"
	switch toolName {
	case "calendar.create_event":
		eyebrow = "Schedule · event created"
	case "calendar.update_event":
		eyebrow = "Schedule · event updated"
	case "calendar.cancel_event", "calendar.delete_event":
		eyebrow = "Schedule · event cancelled"
	}
	card := AssistantCardView{
		Kind:    "event",
		Eyebrow: eyebrow,
		Event: &AssistantEventView{
			Title:    assistantStringValue(eventMap["summary"]),
			When:     assistantCalendarEventWhen(eventMap),
			Calendar: assistantStringValue(data["calendarId"]),
		},
		Success: strings.TrimSpace(resultText),
	}
	if card.Event.Calendar == "" {
		card.Event.Calendar = assistantStringValue(eventMap["calendarId"])
	}
	if card.Event.Title == "" && card.Success == "" {
		return AssistantCardView{}, false
	}
	return card, true
}

func assistantCalendarEventRow(index int, event map[string]any) AssistantCardRowView {
	return AssistantCardRowView{
		Index:   index,
		Sender:  assistantStringValue(event["calendarId"]),
		Subject: assistantStringValue(event["summary"]),
		Detail:  assistantStringValue(event["location"]),
		Meta:    assistantCalendarEventWhen(event),
	}
}

func assistantCalendarEventWhen(event map[string]any) string {
	start, _ := assistantNestedMap(event["start"])
	end, _ := assistantNestedMap(event["end"])
	startValue := assistantStringValue(start["dateTime"])
	if startValue == "" {
		startValue = assistantStringValue(start["date"])
	}
	endValue := assistantStringValue(end["dateTime"])
	if endValue == "" {
		endValue = assistantStringValue(end["date"])
	}
	if startValue == "" {
		return ""
	}
	startDisplay := calendarDisplayTime(startValue)
	if endValue == "" {
		return startDisplay
	}
	return startDisplay + "-" + calendarDisplayTime(endValue)
}

func assistantNestedMap(v any) (map[string]any, bool) {
	if v == nil {
		return nil, false
	}
	switch value := v.(type) {
	case map[string]any:
		return value, true
	default:
		return nil, false
	}
}

func assistantIntValue(v any) int {
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return int(n)
		}
	case string:
		if n, err := json.Number(strings.TrimSpace(value)).Int64(); err == nil {
			return int(n)
		}
	}
	return 0
}

func assistantThreadCard(thread gmailThreadResult) AssistantCardView {
	if len(thread.Messages) == 0 {
		return AssistantCardView{}
	}
	latest := thread.Messages[0]
	eyebrow := "Thread"
	if sender := assistantSenderName(latest.From); sender != "" {
		eyebrow = "Thread · " + sender
	}
	body := strings.TrimSpace(latest.BodyText)
	if body == "" {
		body = strings.TrimSpace(latest.Snippet)
	}
	return AssistantCardView{
		Kind:    "thread",
		Eyebrow: eyebrow,
		Title:   latest.Subject,
		Body:    body,
	}
}

func assistantDraftCard(data map[string]any, thread *gmailThreadResult) (AssistantCardView, bool) {
	preview := assistantStringValue(data["preview"])
	if preview == "" {
		return AssistantCardView{}, false
	}
	channel := strings.TrimSpace(assistantStringValue(data["channel"]))
	to := strings.TrimSpace(assistantStringValue(data["to"]))
	subject := strings.TrimSpace(assistantStringValue(data["subject"]))
	eyebrow := "Draft · ready for review"
	if thread != nil && len(thread.Messages) > 0 {
		to = assistantDefaultString(to, assistantSenderEmail(thread.Messages[0].From))
		subject = assistantDefaultString(subject, thread.Messages[0].Subject)
	}
	if channel == "whatsapp" {
		to = assistantStringValue(data["contact"])
		subject = assistantDefaultString(assistantStringValue(data["thread_id"]), "WhatsApp thread")
		eyebrow = "WhatsApp · draft ready"
	}
	return AssistantCardView{
		Kind:    "draft",
		Eyebrow: eyebrow,
		Draft: &AssistantDraftView{
			To:      to,
			Subject: subject,
			Body:    preview,
		},
		Buttons: []AssistantInlineButtonView{
			{ID: "draft-send", Label: "y send", Tone: "confirm"},
			{ID: "draft-edit", Label: "e edit", Tone: "warn"},
			{ID: "draft-discard", Label: "n discard"},
		},
	}, true
}

func assistantExtractedActionCards(actions ExtractedActions) []AssistantCardView {
	var cards []AssistantCardView
	if len(actions.MeetingReqs) > 0 {
		req := actions.MeetingReqs[0]
		detail := ""
		if len(req.Participants) > 0 {
			detail = "participants: " + strings.Join(req.Participants, ", ")
		}
		cards = append(cards, AssistantCardView{
			Kind:    "meeting",
			Eyebrow: fmt.Sprintf("Found · %d meeting request", len(actions.MeetingReqs)),
			Body:    strings.TrimSpace(req.Subject + "\n" + detail),
		})
		cards = append(cards, AssistantCardView{
			Kind:    "event",
			Eyebrow: "Calendar · event ready",
			Event: &AssistantEventView{
				Title:    req.Subject,
				When:     assistantMeetingWindow(req),
				Calendar: "primary",
				Context:  actions.Summary,
			},
			Buttons: []AssistantInlineButtonView{
				{ID: "event-create", Label: "y create", Tone: "confirm"},
				{ID: "event-skip", Label: "n skip"},
			},
		})
		return cards
	}
	if strings.TrimSpace(actions.Summary) != "" {
		cards = append(cards, AssistantCardView{
			Kind:    "summary",
			Eyebrow: "Actions",
			Body:    actions.Summary,
		})
	}
	return cards
}

func assistantDownloadSummaryCard(downloads []gmailAttachmentDownloadResult) AssistantCardView {
	if len(downloads) == 0 {
		return AssistantCardView{}
	}
	dir := downloads[0].SavedPath
	if idx := strings.LastIndexAny(dir, `/\`); idx >= 0 {
		dir = dir[:idx+1]
	}
	return AssistantCardView{
		Kind:    "download",
		Success: fmt.Sprintf("✓ %d files saved to %s", len(downloads), dir),
		Note:    `next time: jot assistant "download invoice attachments"`,
	}
}

func assistantButtonSuccessText(label string) string {
	switch {
	case strings.Contains(strings.ToLower(label), "send"):
		return "✓ reply sent"
	case strings.Contains(strings.ToLower(label), "create"):
		return "✓ event created"
	case strings.Contains(strings.ToLower(label), "save"):
		return "✓ files saved"
	default:
		return "✓ action updated"
	}
}

func assistantSenderName(value string) string {
	if addr, err := mail.ParseAddress(value); err == nil {
		if strings.TrimSpace(addr.Name) != "" {
			return strings.TrimSpace(addr.Name)
		}
		if strings.TrimSpace(addr.Address) != "" {
			return strings.TrimSpace(addr.Address)
		}
	}
	return strings.TrimSpace(value)
}

func assistantSenderEmail(value string) string {
	if addr, err := mail.ParseAddress(value); err == nil && strings.TrimSpace(addr.Address) != "" {
		return strings.TrimSpace(addr.Address)
	}
	return strings.TrimSpace(value)
}

func assistantRelativeMailTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now().In(t.Location())
	if now.Year() == t.Year() && now.YearDay() == t.YearDay() {
		return t.Format("3:04pm")
	}
	return t.Format("Jan 2")
}

func assistantFormatKB(size int64) string {
	if size <= 0 {
		return ""
	}
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	return fmt.Sprintf("%d KB", (size+1023)/1024)
}

func assistantMeetingWindow(req MeetingRequest) string {
	if len(req.ProposedTimes) == 0 {
		return "pending"
	}
	slot := req.ProposedTimes[0]
	if slot.Start.IsZero() && slot.Raw != "" {
		return slot.Raw
	}
	if slot.Start.IsZero() {
		return "pending"
	}
	if slot.End.IsZero() {
		return slot.Start.Format("Mon Jan 2 · 3:04pm")
	}
	return fmt.Sprintf("%s-%s", slot.Start.Format("Mon Jan 2 · 3:04pm"), slot.End.Format("3:04pm"))
}

func assistantCompactLines(lines []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	return out
}

func assistantStringValue(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func assistantBoolValue(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		value = strings.ToLower(strings.TrimSpace(value))
		return value == "1" || value == "true" || value == "yes"
	default:
		return false
	}
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func mapGmailSummaryQuery(args []string, prompt string) string {
	var parts []string
	for i := 0; i < len(args); i++ {
		switch strings.ToLower(strings.TrimSpace(args[i])) {
		case "--unread":
			parts = append(parts, "is:unread")
		case "--today":
			parts = append(parts, "newer_than:1d")
		case "--last":
			if i+1 < len(args) {
				parts = append(parts, "newer_than:"+normalizeRelativeWindow(args[i+1]))
				i++
			}
		default:
			if !strings.HasPrefix(args[i], "-") {
				parts = append(parts, args[i])
			}
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, " ")
	}
	if q := mapNLToGmailQuery(prompt); q != "" {
		return q
	}
	return "is:unread"
}

func normalizeRelativeWindow(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "7d"
	}
	if strings.HasSuffix(value, "d") || strings.HasSuffix(value, "w") || strings.HasSuffix(value, "m") {
		return value
	}
	return value + "d"
}

func renderAssistantHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot assistant", "CLI-native assistant runtime for Gmail and future connected tools.")
	writeUsageSection(&b, style, []string{
		"jot assistant",
		"jot assistant <request>",
		"jot assistant --onboarding",
		"jot assistant auth gmail",
		"jot assistant auth browser",
		"jot assistant auth whatsapp",
		"jot assistant channels status",
		"jot assistant channels connect <channel>",
		"jot assistant status",
		"jot assistant browser status",
		"jot assistant browser connect",
		"jot assistant browser disconnect",
		"jot assistant gmail search <query>",
		"jot assistant gmail summarize --unread --today",
		"jot assistant gmail attachments --last 30d --save ./invoices",
	}, []string{
		"`jot assistant` starts a REPL-style session.",
		"`jot assistant <request>` runs one instruction and exits.",
		"`jot assistant --onboarding` runs the guided first-run setup for provider, Gmail, the browser computer, and optional messaging channels.",
		"`--format json` emits machine-readable output.",
		"`--verbose` prints tool calls as they run.",
		"`--ui` opens the local assistant viewer.",
	})
	writeFlagSection(&b, style, []helpFlag{
		{name: "--provider ollama|openai|anthropic", description: "Choose the model provider. Defaults to ollama."},
		{name: "--model <name>", description: "Choose the provider model. Defaults to the configured model."},
		{name: "--format text|json", description: "Render text for the terminal or JSON for scripting."},
		{name: "--verbose", description: "Show tool calls and progress lines."},
		{name: "--no-confirm", description: "Bypass confirmations except for delete operations."},
		{name: "--cap gmail|calendar|fs", description: "Scope the assistant to one capability."},
		{name: "--ui", description: "Open the local viewer shell."},
		{name: "--onboarding", description: "Run guided setup for provider, model, API key, Gmail, the browser computer, and optional messaging channels."},
	})
	writeExamplesSection(&b, style, []string{
		"jot assistant --onboarding",
		"jot assistant auth gmail",
		"jot assistant auth browser",
		"jot assistant auth whatsapp",
		"jot assistant channels status",
		"jot assistant channels connect instagram",
		"jot assistant status",
		"jot assistant browser status",
		"jot assistant browser connect",
		`jot assistant "summarize my unread emails from today"`,
		`jot assistant "help me integrate with gmail"`,
		`jot assistant "setup whatsapp for me"`,
		`jot assistant "find invoice attachments from the last 30 days and save them to ./invoices"`,
		`jot assistant "read the latest thread from Alice and draft a reply"`,
		`jot assistant "identify meeting requests in emails from this week"`,
		`jot assistant --format json "extract tasks from unread emails"`,
		`jot assistant --verbose "show emails needing action"`,
		"jot assistant gmail search \"from:alice newer_than:7d\"",
		"jot assistant gmail summarize --unread --today",
		"jot assistant gmail attachments --last 30d --save ./invoices",
		"jot assistant --ui",
	})
	return b.String()
}
