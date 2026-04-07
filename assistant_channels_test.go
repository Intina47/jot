package main

import "testing"

func TestAssistantChannelKinds(t *testing.T) {
	kinds := AssistantChannelKinds()
	if len(kinds) != 4 {
		t.Fatalf("expected 4 channel kinds, got %d", len(kinds))
	}
	if kinds[0] != AssistantChannelWhatsApp || kinds[1] != AssistantChannelTelegram || kinds[2] != AssistantChannelDiscord || kinds[3] != AssistantChannelInstagram {
		t.Fatalf("unexpected channel order: %#v", kinds)
	}
}

func TestAssistantChannelKindFromString(t *testing.T) {
	cases := []struct {
		in   string
		want AssistantChannelKind
		ok   bool
	}{
		{in: "WhatsApp", want: AssistantChannelWhatsApp, ok: true},
		{in: "whatsapp-web", want: AssistantChannelWhatsApp, ok: true},
		{in: "tg", want: AssistantChannelTelegram, ok: true},
		{in: "discord", want: AssistantChannelDiscord, ok: true},
		{in: "insta", want: AssistantChannelInstagram, ok: true},
		{in: "unknown", want: "", ok: false},
	}
	for _, tc := range cases {
		got, ok := AssistantChannelKindFromString(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("AssistantChannelKindFromString(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestAssistantChannelDefaultConfigAndNormalize(t *testing.T) {
	cfg := AssistantChannelDefaultConfig("C:/Users/mamba/AppData/Roaming/jot", AssistantChannelInstagram)
	if cfg.Kind != string(AssistantChannelInstagram) {
		t.Fatalf("unexpected kind: %q", cfg.Kind)
	}
	if cfg.ConnectURL != AssistantChannelDefaultConnectURL(AssistantChannelInstagram) {
		t.Fatalf("unexpected connect URL: %q", cfg.ConnectURL)
	}
	if cfg.VerifyURL != AssistantChannelDefaultVerifyURL(AssistantChannelInstagram) {
		t.Fatalf("unexpected verify URL: %q", cfg.VerifyURL)
	}
	if cfg.BrowserProfilePath == "" {
		t.Fatal("expected default browser profile path")
	}
	if cfg.BridgeStateDir == "" {
		t.Fatal("expected default bridge state path")
	}

	byKind := assistantDefaultChannels("C:/Users/mamba/AppData/Roaming/jot")
	if len(byKind) != 4 {
		t.Fatalf("expected 4 default channels, got %d", len(byKind))
	}
	if _, ok := byKind[string(AssistantChannelDiscord)]; !ok {
		t.Fatal("expected discord default channel")
	}

	cfg = AssistantChannelConfig{
		Kind:                "IG",
		AllowedPeers:        []string{"  alice  ", "Bob", "alice", ""},
		LastSeenMessageID:   "  msg-123  ",
		PollIntervalSeconds: 0,
	}
	AssistantChannelNormalizeConfig(&cfg, "C:/Users/mamba/AppData/Roaming/jot")
	if cfg.Kind != string(AssistantChannelInstagram) {
		t.Fatalf("normalized kind = %q, want %q", cfg.Kind, AssistantChannelInstagram)
	}
	if cfg.PollIntervalSeconds != 30 {
		t.Fatalf("normalized poll interval = %d, want 30", cfg.PollIntervalSeconds)
	}
	if len(cfg.AllowedPeers) != 2 || cfg.AllowedPeers[0] != "alice" || cfg.AllowedPeers[1] != "Bob" {
		t.Fatalf("normalized allowed peers = %#v", cfg.AllowedPeers)
	}
	if cfg.LastSeenMessageID != "msg-123" {
		t.Fatalf("normalized last seen message id = %q", cfg.LastSeenMessageID)
	}
	if cfg.BridgeStateDir == "" {
		t.Fatal("expected normalized bridge state path")
	}
}

func TestAssistantChannelStatusHelpers(t *testing.T) {
	cfg := AssistantChannelConfig{
		Kind:      string(AssistantChannelTelegram),
		Enabled:   true,
		Onboarded: true,
		Connected: true,
	}
	status := AssistantChannelStatusForConfig(cfg)
	if !status.Ready || status.State != "connected" {
		t.Fatalf("unexpected ready status: %#v", status)
	}
	if got := AssistantChannelStatusLine(cfg); got != "Telegram: connected" {
		t.Fatalf("unexpected status line: %q", got)
	}

	cfg.Connected = false
	status = AssistantChannelStatusForConfig(cfg)
	if status.State != "needs-sign-in" {
		t.Fatalf("unexpected disconnected state: %#v", status)
	}

	cfg.Enabled = false
	status = AssistantChannelStatusForConfig(cfg)
	if status.State != "disabled" {
		t.Fatalf("unexpected disabled state: %#v", status)
	}

	wa := AssistantChannelConfig{
		Kind:    string(AssistantChannelWhatsApp),
		Enabled: true,
	}
	status = AssistantChannelStatusForConfig(wa)
	if status.State != "needs-bridge" {
		t.Fatalf("unexpected whatsapp bridge state: %#v", status)
	}
}

func TestAssistantChannelBrowserHeuristics(t *testing.T) {
	positive := map[AssistantChannelKind]AssistantChannelBrowserProbe{
		AssistantChannelWhatsApp:  {URL: "https://web.whatsapp.com/", Text: "keep your phone connected to the internet"},
		AssistantChannelTelegram:  {URL: "https://web.telegram.org/k/", Text: "messages saved messages"},
		AssistantChannelDiscord:   {URL: "https://discord.com/channels/@me", Text: "direct messages friends"},
		AssistantChannelInstagram: {URL: "https://www.instagram.com/direct/inbox/", Text: "direct messages inbox"},
	}
	for kind, probe := range positive {
		if !assistantChannelLooksConnected(string(kind), BrowserPageSnapshot{URL: probe.URL, Title: probe.Title, Text: probe.Text}) {
			t.Fatalf("expected %s probe to look connected", kind)
		}
	}

	negative := map[AssistantChannelKind]AssistantChannelBrowserProbe{
		AssistantChannelWhatsApp:  {URL: "https://web.whatsapp.com/", Text: "scan this code to log in"},
		AssistantChannelTelegram:  {URL: "https://web.telegram.org/k/", Text: "log in by qr code"},
		AssistantChannelDiscord:   {URL: "https://discord.com/app", Text: "log in"},
		AssistantChannelInstagram: {URL: "https://www.instagram.com/direct/inbox/", Text: "sign up"},
	}
	for kind, probe := range negative {
		if assistantChannelLooksConnected(string(kind), BrowserPageSnapshot{URL: probe.URL, Title: probe.Title, Text: probe.Text}) {
			t.Fatalf("expected %s probe to look disconnected", kind)
		}
	}
}

func TestAssistantChannelBridgeEnv(t *testing.T) {
	env := assistantChannelBridgeEnv(assistantChannelWhatsApp, "C:/Users/mamba/AppData/Roaming/jot/channels/whatsapp-bridge-state")
	found := false
	for _, item := range env {
		if item == "JOT_WHATSAPP_BRIDGE_DIR=C:/Users/mamba/AppData/Roaming/jot/channels/whatsapp-bridge-state" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected WhatsApp bridge env to include JOT_WHATSAPP_BRIDGE_DIR")
	}

	env = assistantChannelBridgeEnv(assistantChannelTelegram, "C:/ignored")
	for _, item := range env {
		if item == "JOT_WHATSAPP_BRIDGE_DIR=C:/ignored" {
			t.Fatal("did not expect non-WhatsApp bridge env override")
		}
	}
}
