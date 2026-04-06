# Jot Local Dev Setup

This file is for bringing Jot up quickly on a fresh local machine.

## 1. Open the Repo

PowerShell:

```powershell
cd C:\Users\mamba\jot
```

Git Bash:

```bash
cd ~/jot
```

## 2. Set Local Build Cache First

This matters on Windows because the default Go cache path has caused `Access is denied` during builds and tests.

PowerShell:

```powershell
$env:GOCACHE="C:\Users\mamba\AppData\Local\Temp\jot-gocache"
```

Git Bash:

```bash
export GOCACHE=/c/Users/mamba/AppData/Local/Temp/jot-gocache
```

## 3. Set Assistant Environment Variables

Only set what you actually use.

### Ollama

If using local Ollama only:

```powershell
$env:JOT_ASSISTANT_PROVIDER="ollama"
$env:JOT_ASSISTANT_MODEL="glm-5:cloud"
```

If using Ollama Cloud:

```powershell
$env:OLLAMA_API_KEY="your-ollama-api-key"
```

### Gmail OAuth

If you want Gmail onboarding and auth available immediately:

```powershell
$env:JOT_GMAIL_CLIENT_ID="your-google-desktop-client-id"
$env:JOT_GMAIL_CLIENT_SECRET="your-google-desktop-client-secret"
```

You can also keep these in your Jot assistant config instead of exporting them every session.

## 4. Optional: WhatsApp Bridge Dependencies

Only needed if you are working on WhatsApp native bridge flows.

```powershell
cd tools\whatsapp-bridge
npm install
cd ..\..
```

## 5. Build the Project

Terminal build:

```powershell
go build
```

Explicit test binary build:

```powershell
go build -o jot-test.exe .
```

Windows GUI build:

```powershell
go build -ldflags="-H windowsgui" -o jot.exe .
```

## 6. Run Tests

Full suite:

```powershell
go test ./...
```

Focused smoke examples:

```powershell
go test -run TestGmailExecuteSendEmail_FallsBackToDraftWhenSendFails -v
go test -run TestEnsureGmailSendReady_ReauthsInlineAndContinues -v
```

## 7. Run the Main Flows

Interactive assistant:

```powershell
.\jot.exe assistant
```

Guided onboarding:

```powershell
.\jot.exe assistant --onboarding
```

Status:

```powershell
.\jot.exe assistant status
```

Browser computer:

```powershell
.\jot.exe assistant browser status
.\jot.exe assistant auth browser
```

Gmail auth:

```powershell
.\jot.exe assistant auth gmail
```

Journal backup test:

```powershell
.\jot.exe assistant "export my jot journal and email it to me"
```

## 8. Versioning

Current local beta line:

- [main.go](./main.go) -> `1.7.2-beta.1`

For local-only beta bumps, update the CLI version in `main.go` first. Do not touch Homebrew, npm, or Chocolatey packaging unless you are preparing a real release.

## 9. Before You Commit

Run in this order:

```powershell
gofmt -w .
go test ./...
go build -o jot-test.exe .
git status --short
```

Then inspect for local-only artifacts. Do not commit:

- `client_secret_*.json`
- local screenshots or random assets
- `.state` output from `tools/whatsapp-bridge`
- temporary binaries unless you intentionally want them tracked

## 10. Push Workflow

Typical flow:

```powershell
git add .
git commit -m "Your message"
git push origin main
```

If you are continuing assistant-heavy work, read these first:

1. [README.md](./README.md)
2. [ASSISTANT.md](./ASSISTANT.md)
3. [agents.md](./agents.md)
