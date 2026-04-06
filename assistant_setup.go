package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type SetupCapability struct {
	Config AssistantConfig
}

func (c *SetupCapability) Name() string { return "setup" }

func (c *SetupCapability) Description() string {
	return "Connect and verify assistant integrations like Gmail, the browser computer, and messaging channels."
}

func (c *SetupCapability) Tools() []Tool {
	return []Tool{
		{
			Name:        "setup.connect_service",
			Description: "Connect or repair an assistant integration such as gmail, browser, whatsapp, telegram, discord, or instagram. This is handled by the assistant runtime because it may open browser auth or local setup flows.",
			ParamSchema: `{"type":"object","properties":{"service":{"type":"string"}},"required":["service"]}`,
		},
		{
			Name:        "setup.status_service",
			Description: "Check whether an assistant integration is connected and ready.",
			ParamSchema: `{"type":"object","properties":{"service":{"type":"string"}},"required":["service"]}`,
		},
	}
}

func (c *SetupCapability) Execute(toolName string, params map[string]any) (ToolResult, error) {
	err := fmt.Errorf("%s is handled by the assistant runtime", strings.TrimSpace(toolName))
	return ToolResult{Success: false, Error: err.Error()}, err
}

func executeAssistantSetupService(ctx context.Context, s *AssistantSession, call AssistantToolCall, in io.Reader, out io.Writer) (ToolResult, error) {
	if ctx != nil && ctx.Err() != nil {
		return ToolResult{Success: false, Error: ctx.Err().Error()}, ctx.Err()
	}
	service := strings.TrimSpace(firstStringParam(call.Params, "service", "channel", "name"))
	service = assistantNormalizeSetupService(service)
	if service == "" {
		err := errors.New("service must be one of gmail, browser, whatsapp, telegram, discord, or instagram")
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	switch strings.ToLower(strings.TrimSpace(call.Tool)) {
	case "setup.status_service":
		return assistantSetupStatusResult(s.Config, service)
	case "setup.connect_service":
		cfg, summary, err := assistantConnectServiceForRuntime(in, out, s.Config, service)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		if err := SaveAssistantConfigFile(cfg); err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		s.Config = cfg
		if caps, err := buildAssistantCapabilities(cfg, "", s.Provider); err == nil {
			s.Capabilities = caps
		}
		return ToolResult{
			Success: true,
			Text:    summary,
			Data: map[string]any{
				"assistant_final": true,
				"service":         service,
				"summary":         summary,
				"connected":       true,
			},
		}, nil
	default:
		err := fmt.Errorf("unknown setup tool %q", call.Tool)
		return ToolResult{Success: false, Error: err.Error()}, err
	}
}

func assistantNormalizeSetupService(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "gmail", "google mail":
		return "gmail"
	case "browser", "browser computer", "forms":
		return "browser"
	default:
		return assistantNormalizeChannelName(value)
	}
}

func assistantSetupStatusResult(cfg AssistantConfig, service string) (ToolResult, error) {
	switch service {
	case "gmail":
		gmail, err := NewGmailCapability(cfg)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		return gmail.Execute("gmail.status", map[string]any{})
	case "browser":
		connected := cfg.BrowserEnabled && cfg.BrowserConnected && assistantBrowserProfileExists(cfg)
		return ToolResult{
			Success: true,
			Text:    map[bool]string{true: "browser computer connected", false: "browser computer not connected"}[connected],
			Data: map[string]any{
				"connected": connected,
				"profile":   strings.TrimSpace(cfg.BrowserProfilePath),
			},
		}, nil
	default:
		settings := assistantChannelConfig(cfg, service)
		connected := settings.Enabled && settings.Connected
		return ToolResult{
			Success: true,
			Text:    fmt.Sprintf("%s %s", assistantChannelDisplayName(service), map[bool]string{true: "connected", false: "not connected"}[connected]),
			Data: map[string]any{
				"connected": connected,
				"bridge":    strings.TrimSpace(settings.BridgeCommand),
				"account":   strings.TrimSpace(settings.AccountLabel),
			},
		}, nil
	}
}

func assistantConnectServiceForRuntime(stdin io.Reader, stdout io.Writer, cfg AssistantConfig, service string) (AssistantConfig, string, error) {
	switch service {
	case "gmail":
		gmail, err := NewGmailCapability(cfg)
		if err != nil {
			return cfg, "", err
		}
		if err := gmail.Authenticate(stdout); err != nil {
			return cfg, "", err
		}
		return cfg, "Gmail connected and ready.", nil
	case "browser":
		next, err := assistantConnectBrowserProfile(stdin, stdout, cfg)
		if err != nil {
			return cfg, "", err
		}
		return next, "Browser computer connected and ready.", nil
	default:
		next, err := assistantConnectMessagingChannel(stdin, stdout, cfg, service)
		if err != nil {
			return cfg, "", err
		}
		settings := assistantChannelConfig(next, service)
		if settings.Enabled && settings.Connected {
			return next, assistantChannelDisplayName(service) + " connected and ready.", nil
		}
		if service == assistantChannelWhatsApp && strings.TrimSpace(settings.BridgeCommand) != "" {
			return next, "WhatsApp bridge is prepared. If pairing is still pending, scan the QR code and ask me to finish setup again.", nil
		}
		return next, assistantChannelDisplayName(service) + " setup is prepared but not fully connected yet.", nil
	}
}

func assistantMaybePrepareWhatsAppBridge(stdout io.Writer, cfg AssistantConfig) (AssistantConfig, error) {
	next := cfg
	settings := assistantChannelConfig(next, assistantChannelWhatsApp)
	if strings.TrimSpace(settings.BridgeCommand) != "" {
		if next.Channels == nil {
			next.Channels = make(map[string]AssistantChannelConfig)
		}
		next.Channels[assistantChannelWhatsApp] = settings
		return next, nil
	}
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return cfg, errors.New("node is required to set up the WhatsApp bridge automatically")
	}
	bridgePath, err := assistantFindBundledWhatsAppBridge()
	if err != nil {
		return cfg, err
	}
	settings.BridgeCommand = nodePath
	settings.BridgeArgs = []string{bridgePath}
	if next.Channels == nil {
		next.Channels = make(map[string]AssistantChannelConfig)
	}
	next.Channels[assistantChannelWhatsApp] = settings
	if stdout != nil {
		ui := newTermUI(stdout)
		_, _ = fmt.Fprintln(stdout, "  "+ui.tdim("configured the bundled local WhatsApp bridge automatically."))
	}
	if err := assistantEnsureWhatsAppBridgeDependencies(stdout, bridgePath); err != nil {
		return next, err
	}
	return next, nil
}

func assistantFindBundledWhatsAppBridge() (string, error) {
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil && strings.TrimSpace(wd) != "" {
		candidates = append(candidates, filepath.Join(wd, "tools", "whatsapp-bridge", "index.mjs"))
	}
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		base := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(base, "tools", "whatsapp-bridge", "index.mjs"),
			filepath.Join(filepath.Dir(base), "tools", "whatsapp-bridge", "index.mjs"),
		)
	}
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if _, ok := seen[strings.ToLower(candidate)]; ok {
			continue
		}
		seen[strings.ToLower(candidate)] = struct{}{}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("could not find the bundled WhatsApp bridge on disk")
}

func assistantEnsureWhatsAppBridgeDependencies(stdout io.Writer, bridgePath string) error {
	bridgeDir := filepath.Dir(strings.TrimSpace(bridgePath))
	required := filepath.Join(bridgeDir, "node_modules", "@whiskeysockets", "baileys", "package.json")
	if info, err := os.Stat(required); err == nil && !info.IsDir() {
		return nil
	}
	npmPath, err := exec.LookPath("npm")
	if err != nil {
		return errors.New("npm is required to install WhatsApp bridge dependencies automatically")
	}
	if stdout != nil {
		ui := newTermUI(stdout)
		_, _ = fmt.Fprintln(stdout, "  "+ui.tdim("installing WhatsApp bridge dependencies..."))
	}
	cmd := exec.Command(npmPath, "install")
	cmd.Dir = bridgeDir
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm install failed for the WhatsApp bridge: %w", err)
	}
	return nil
}
