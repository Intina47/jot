package main

import (
	"path/filepath"
	"strings"
)

type AssistantChannelKind string

const (
	AssistantChannelWhatsApp  AssistantChannelKind = "whatsapp"
	AssistantChannelTelegram  AssistantChannelKind = "telegram"
	AssistantChannelDiscord   AssistantChannelKind = "discord"
	AssistantChannelInstagram AssistantChannelKind = "instagram"
)

const (
	assistantChannelWhatsApp  = string(AssistantChannelWhatsApp)
	assistantChannelTelegram  = string(AssistantChannelTelegram)
	assistantChannelDiscord   = string(AssistantChannelDiscord)
	assistantChannelInstagram = string(AssistantChannelInstagram)
)

type AssistantChannelBrowserProbe struct {
	URL   string
	Title string
	Text  string
}

type AssistantChannelConfig struct {
	Kind                string   `json:"kind,omitempty"`
	Enabled             bool     `json:"enabled,omitempty"`
	Onboarded           bool     `json:"onboarded,omitempty"`
	Connected           bool     `json:"connected,omitempty"`
	BridgeCommand       string   `json:"bridgeCommand,omitempty"`
	BridgeArgs          []string `json:"bridgeArgs,omitempty"`
	ReplyPolicy         string   `json:"replyPolicy,omitempty"`
	BrowserProfilePath  string   `json:"browserProfilePath,omitempty"`
	ConnectURL          string   `json:"connectUrl,omitempty"`
	VerifyURL           string   `json:"verifyUrl,omitempty"`
	AccountLabel        string   `json:"accountLabel,omitempty"`
	AllowedContacts     []string `json:"allowedContacts,omitempty"`
	AllowedPeers        []string `json:"allowedPeers,omitempty"`
	LastCursor          string   `json:"lastCursor,omitempty"`
	LastSeenMessageID   string   `json:"lastSeenMessageId,omitempty"`
	PollIntervalSecs    int      `json:"pollIntervalSecs,omitempty"`
	PollIntervalSeconds int      `json:"pollIntervalSeconds,omitempty"`
}

type AssistantChannelStatus struct {
	Ready bool   `json:"ready"`
	State string `json:"state"`
}

func AssistantChannelKinds() []AssistantChannelKind {
	return []AssistantChannelKind{
		AssistantChannelWhatsApp,
		AssistantChannelTelegram,
		AssistantChannelDiscord,
		AssistantChannelInstagram,
	}
}

func AssistantChannelKindFromString(value string) (AssistantChannelKind, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "whatsapp", "whatsapp-web", "wa":
		return AssistantChannelWhatsApp, true
	case "telegram", "tg":
		return AssistantChannelTelegram, true
	case "discord":
		return AssistantChannelDiscord, true
	case "instagram", "insta", "ig":
		return AssistantChannelInstagram, true
	default:
		return "", false
	}
}

func assistantSupportedChannels() []string {
	kinds := AssistantChannelKinds()
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, string(kind))
	}
	return out
}

func assistantNormalizeChannelName(value string) string {
	if kind, ok := AssistantChannelKindFromString(value); ok {
		return string(kind)
	}
	value = strings.ToLower(strings.TrimSpace(value))
	for _, channel := range assistantSupportedChannels() {
		if value == channel {
			return channel
		}
	}
	return ""
}

func assistantDefaultChannels(configDir string) map[string]AssistantChannelConfig {
	out := make(map[string]AssistantChannelConfig, len(assistantSupportedChannels()))
	for _, channel := range assistantSupportedChannels() {
		out[channel] = assistantDefaultChannelConfig(configDir, channel)
	}
	return out
}

func assistantDefaultChannelConfig(configDir, channel string) AssistantChannelConfig {
	return AssistantChannelConfig{
		Kind:                channel,
		ReplyPolicy:         "draft",
		BrowserProfilePath:  filepath.Join(configDir, "channels", channel+"-browser-profile"),
		ConnectURL:          assistantChannelConnectURL(channel),
		VerifyURL:           assistantChannelVerifyURL(channel),
		PollIntervalSecs:    30,
		PollIntervalSeconds: 30,
	}
}

func assistantNormalizeChannels(cfg *AssistantConfig, configDir string) {
	if cfg.Channels == nil {
		cfg.Channels = assistantDefaultChannels(configDir)
		return
	}
	for _, channel := range assistantSupportedChannels() {
		item := cfg.Channels[channel]
		defaults := assistantDefaultChannelConfig(configDir, channel)
		item.Kind = assistantNormalizeChannelName(assistantDefaultString(item.Kind, channel))
		if strings.TrimSpace(item.BrowserProfilePath) == "" {
			item.BrowserProfilePath = defaults.BrowserProfilePath
		}
		if strings.TrimSpace(item.ConnectURL) == "" {
			item.ConnectURL = defaults.ConnectURL
		}
		if strings.TrimSpace(item.VerifyURL) == "" {
			item.VerifyURL = defaults.VerifyURL
		}
		item.BridgeCommand = strings.TrimSpace(item.BridgeCommand)
		item.BridgeArgs = append([]string(nil), assistantNormalizeStringList(item.BridgeArgs)...)
		item.ReplyPolicy = strings.TrimSpace(assistantDefaultString(item.ReplyPolicy, defaults.ReplyPolicy))
		if item.PollIntervalSeconds <= 0 {
			item.PollIntervalSeconds = item.PollIntervalSecs
		}
		if item.PollIntervalSecs <= 0 {
			item.PollIntervalSecs = item.PollIntervalSeconds
		}
		if item.PollIntervalSecs <= 0 {
			item.PollIntervalSecs = defaults.PollIntervalSecs
		}
		if item.PollIntervalSeconds <= 0 {
			item.PollIntervalSeconds = defaults.PollIntervalSeconds
		}
		item.AccountLabel = strings.TrimSpace(item.AccountLabel)
		item.LastCursor = strings.TrimSpace(item.LastCursor)
		item.LastSeenMessageID = strings.TrimSpace(assistantDefaultString(item.LastSeenMessageID, item.LastCursor))
		item.AllowedContacts = assistantNormalizeStringList(item.AllowedContacts)
		if len(item.AllowedContacts) == 0 && len(item.AllowedPeers) > 0 {
			item.AllowedContacts = assistantNormalizeStringList(item.AllowedPeers)
		}
		item.AllowedPeers = assistantNormalizeStringList(item.AllowedContacts)
		cfg.Channels[channel] = item
	}
}

func assistantChannelConfig(cfg AssistantConfig, channel string) AssistantChannelConfig {
	channel = assistantNormalizeChannelName(channel)
	if channel == "" {
		return AssistantChannelConfig{}
	}
	if cfg.Channels != nil {
		if item, ok := cfg.Channels[channel]; ok {
			return item
		}
	}
	baseDir := filepath.Dir(strings.TrimSpace(cfg.MemoryPath))
	if baseDir == "." || baseDir == "" {
		baseDir = filepath.Dir(strings.TrimSpace(cfg.AttachmentSaveDir))
	}
	return assistantDefaultChannelConfig(baseDir, channel)
}

func AssistantChannelDefaultConfig(configDir string, kind AssistantChannelKind) AssistantChannelConfig {
	return assistantDefaultChannelConfig(configDir, string(kind))
}

func AssistantChannelDefaultConnectURL(kind AssistantChannelKind) string {
	return assistantChannelConnectURL(string(kind))
}

func AssistantChannelDefaultVerifyURL(kind AssistantChannelKind) string {
	return assistantChannelVerifyURL(string(kind))
}

func AssistantChannelNormalizeConfig(cfg *AssistantChannelConfig, configDir string) {
	if cfg == nil {
		return
	}
	kind := assistantNormalizeChannelName(cfg.Kind)
	if kind == "" {
		kind = assistantChannelInstagram
	}
	defaults := assistantDefaultChannelConfig(configDir, kind)
	cfg.Kind = kind
	cfg.BrowserProfilePath = assistantDefaultString(strings.TrimSpace(cfg.BrowserProfilePath), defaults.BrowserProfilePath)
	cfg.ConnectURL = assistantDefaultString(strings.TrimSpace(cfg.ConnectURL), defaults.ConnectURL)
	cfg.VerifyURL = assistantDefaultString(strings.TrimSpace(cfg.VerifyURL), defaults.VerifyURL)
	cfg.BridgeCommand = strings.TrimSpace(cfg.BridgeCommand)
	cfg.BridgeArgs = append([]string(nil), assistantNormalizeStringList(cfg.BridgeArgs)...)
	cfg.ReplyPolicy = strings.TrimSpace(assistantDefaultString(cfg.ReplyPolicy, defaults.ReplyPolicy))
	cfg.LastSeenMessageID = strings.TrimSpace(assistantDefaultString(cfg.LastSeenMessageID, cfg.LastCursor))
	cfg.LastCursor = strings.TrimSpace(assistantDefaultString(cfg.LastCursor, cfg.LastSeenMessageID))
	if cfg.PollIntervalSeconds <= 0 {
		cfg.PollIntervalSeconds = cfg.PollIntervalSecs
	}
	if cfg.PollIntervalSeconds <= 0 {
		cfg.PollIntervalSeconds = defaults.PollIntervalSeconds
	}
	cfg.PollIntervalSecs = cfg.PollIntervalSeconds
	cfg.AllowedContacts = assistantNormalizeStringList(append(cfg.AllowedContacts, cfg.AllowedPeers...))
	cfg.AllowedPeers = append([]string(nil), cfg.AllowedContacts...)
}

func assistantConnectedChannelNames(cfg AssistantConfig) []string {
	var out []string
	for _, channel := range assistantSupportedChannels() {
		item := assistantChannelConfig(cfg, channel)
		if item.Enabled && item.Connected {
			out = append(out, channel)
		}
	}
	return out
}

func assistantParseChannelSelection(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	})
	for _, field := range fields {
		name := assistantNormalizeChannelName(field)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func assistantChannelDisplayName(channel string) string {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "gmail":
		return "Gmail"
	case "browser":
		return "browser computer"
	}
	switch assistantNormalizeChannelName(channel) {
	case assistantChannelWhatsApp:
		return "WhatsApp"
	case assistantChannelTelegram:
		return "Telegram"
	case assistantChannelDiscord:
		return "Discord"
	case assistantChannelInstagram:
		return "Instagram"
	default:
		return strings.TrimSpace(channel)
	}
}

func assistantChannelConnectURL(channel string) string {
	switch assistantNormalizeChannelName(channel) {
	case assistantChannelWhatsApp:
		return "https://web.whatsapp.com/"
	case assistantChannelTelegram:
		return "https://web.telegram.org/a/"
	case assistantChannelDiscord:
		return "https://discord.com/channels/@me"
	case assistantChannelInstagram:
		return "https://www.instagram.com/direct/inbox/"
	default:
		return ""
	}
}

func assistantChannelVerifyURL(channel string) string {
	return assistantChannelConnectURL(channel)
}

func assistantChannelLooksConnected(channel string, snapshot BrowserPageSnapshot) bool {
	text := strings.ToLower(strings.TrimSpace(snapshot.Title + " " + snapshot.Text))
	urlText := strings.ToLower(strings.TrimSpace(snapshot.URL))
	switch assistantNormalizeChannelName(channel) {
	case assistantChannelWhatsApp:
		if strings.Contains(text, "use whatsapp on your computer") || strings.Contains(text, "scan the qr code") || strings.Contains(text, "scan this code to log in") {
			return false
		}
		return strings.Contains(urlText, "whatsapp") && (strings.Contains(text, "chats") || strings.Contains(text, "messages") || strings.Contains(text, "status") || strings.Contains(text, "keep your phone connected to the internet"))
	case assistantChannelTelegram:
		if strings.Contains(text, "log in to telegram") || strings.Contains(text, "your phone number") || strings.Contains(text, "log in by qr code") {
			return false
		}
		return strings.Contains(urlText, "telegram") && (strings.Contains(text, "telegram") || strings.Contains(text, "saved messages") || strings.Contains(text, "new message"))
	case assistantChannelDiscord:
		if strings.Contains(text, "welcome back") || strings.Contains(text, "login") {
			return false
		}
		return strings.Contains(urlText, "discord.com") && (strings.Contains(text, "friends") || strings.Contains(text, "direct messages") || strings.Contains(text, "nitro"))
	case assistantChannelInstagram:
		if strings.Contains(text, "log in") || strings.Contains(text, "sign up") {
			return false
		}
		return strings.Contains(urlText, "instagram.com") && (strings.Contains(text, "messages") || strings.Contains(text, "send message") || strings.Contains(text, "inbox"))
	default:
		return false
	}
}

func assistantChannelAccountLabel(channel string, snapshot BrowserPageSnapshot) string {
	parts := []string{snapshot.Title}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch assistantNormalizeChannelName(channel) {
		case assistantChannelWhatsApp:
			part = strings.TrimSuffix(part, " WhatsApp")
		case assistantChannelTelegram:
			part = strings.TrimSuffix(part, " Telegram")
		case assistantChannelDiscord:
			part = strings.TrimSuffix(part, " | Discord")
		case assistantChannelInstagram:
			part = strings.TrimSuffix(part, " • Instagram photos and videos")
		}
		part = strings.TrimSpace(part)
		if part != "" && !strings.EqualFold(part, assistantChannelDisplayName(channel)) {
			return part
		}
	}
	return ""
}

func assistantChannelStatusMap(cfg AssistantConfig) map[string]map[string]any {
	out := make(map[string]map[string]any, len(assistantSupportedChannels()))
	for _, channel := range assistantSupportedChannels() {
		item := assistantChannelConfig(cfg, channel)
		out[channel] = map[string]any{
			"enabled":   item.Enabled,
			"onboarded": item.Onboarded,
			"connected": item.Enabled && item.Connected && (strings.TrimSpace(item.BrowserProfilePath) != "" || strings.TrimSpace(item.BridgeCommand) != ""),
			"profile":   strings.TrimSpace(item.BrowserProfilePath),
			"account":   strings.TrimSpace(item.AccountLabel),
			"bridge":    strings.TrimSpace(item.BridgeCommand),
		}
	}
	return out
}

func AssistantChannelStatusForConfig(cfg AssistantChannelConfig) AssistantChannelStatus {
	AssistantChannelNormalizeConfig(&cfg, filepath.Dir(cfg.BrowserProfilePath))
	status := AssistantChannelStatus{State: "disabled"}
	switch {
	case cfg.Kind == assistantChannelWhatsApp && cfg.Enabled && strings.TrimSpace(cfg.BridgeCommand) == "":
		status.State = "needs-bridge"
	case cfg.Enabled && cfg.Connected && (strings.TrimSpace(cfg.BrowserProfilePath) != "" || strings.TrimSpace(cfg.BridgeCommand) != ""):
		status.Ready = true
		status.State = "connected"
	case cfg.Enabled:
		status.State = "needs-sign-in"
	}
	return status
}

func AssistantChannelStatusLine(cfg AssistantChannelConfig) string {
	status := AssistantChannelStatusForConfig(cfg)
	return assistantChannelDisplayName(cfg.Kind) + ": " + status.State
}

func assistantChannelHasNativeBridge(cfg AssistantConfig, channel string) bool {
	item := assistantChannelConfig(cfg, channel)
	return strings.TrimSpace(item.BridgeCommand) != ""
}

func assistantNormalizeStringList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}
