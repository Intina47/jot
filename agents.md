# Jot Agent Handoff

This file is for future AI agents working on Jot.

## Product in One Paragraph

Jot started as a terminal-first notebook and local document viewer. It now also includes `jot assistant`, a local assistant runtime that can search and act in Gmail, manage Google Calendar, drive a browser computer for form work, export the Jot journal, and scaffold native messaging channels. The product direction is pragmatic: local-first, useful, minimal, and capability-driven instead of dashboard-driven.

## Current Version

- CLI version in code: `1.7.2-beta.1`
- This is a local beta patch line, not a polished public release

## Core Product Principles

- Terminal-first
- Local-first
- Real actions over fake chat
- Model-led reasoning where possible
- Avoid regex-heavy feature logic
- Preserve trust on high-risk actions with confirmation
- Keep UX simple even when the capability underneath is complex

## Main Surfaces

### 1. Notebook and Viewer

- `jot`
- `jot init`
- `jot list`
- `jot open`

Key files:
- [main.go](./main.go)
- [write.go](./write.go)
- [main_windows.go](./main_windows.go)

Journal storage:
- `%USERPROFILE%\\.jot\\journal.jsonl`
- legacy migration path may still involve `journal.txt`

### 2. Assistant Runtime

Entry point:
- `jot assistant`

Main files:
- [assistant.go](./assistant.go)
- [assistant_runtime.go](./assistant_runtime.go)
- [assistant_output.go](./assistant_output.go)
- [assistant_provider.go](./assistant_provider.go)
- [assistant_test.go](./assistant_test.go)

The assistant currently supports:
- Gmail
- Calendar
- browser-computer form work
- journal backup export
- memory scaffolding
- channel scaffolding

### 3. Gmail + Calendar

Main files:
- [assistant_gmail.go](./assistant_gmail.go)
- [assistant_backup.go](./assistant_backup.go)

What works now:
- Gmail search and read
- attachment reading
- drafting replies
- sending new emails
- Gmail send fallback to draft when sending fails
- Calendar free/busy, find, create, update, cancel
- journal backup export and email-to-self flow

Important behavior:
- if Gmail send access is missing, the runtime can now re-auth inline and continue
- if sending still fails, Jot falls back to saving a Gmail draft

### 4. Browser Computer and Forms

Main files:
- [assistant_browser.go](./assistant_browser.go)
- [assistant_forms.go](./assistant_forms.go)

Current shape:
- browser-first, not scraper-first
- question-by-question fill loop
- DOM semantics plus screenshot perception hooks
- completion audit before submit
- direct form URL support, not just email-linked forms

This is still beta-quality, especially across arbitrary websites.

### 5. Memory and Channels

Main files:
- [assistant_memory.go](./assistant_memory.go)
- [assistant_channels.go](./assistant_channels.go)
- [assistant_channel_runtime.go](./assistant_channel_runtime.go)
- [assistant_channel_whatsapp.go](./assistant_channel_whatsapp.go)
- [tools/whatsapp-bridge](./tools/whatsapp-bridge)

Current state:
- shared local memory scaffolding exists
- WhatsApp native bridge scaffolding exists
- Baileys-backed bridge process exists under `tools/whatsapp-bridge`
- messaging is not fully production-ready yet

## Important UX Decisions Already Made

- `jot assistant --onboarding` should be the main path
- browser computer uses a dedicated local browser profile
- Gmail/Calendar are currently for local/private use, not public turnkey onboarding
- forms can come from direct links or email context
- journal backup can be exported and emailed
- if a user explicitly asks to email something, Jot should prefer send over draft

## Files to Understand First

If you are new to the repo, read these first:

1. [README.md](./README.md)
2. [ASSISTANT.md](./ASSISTANT.md)
3. [assistant.go](./assistant.go)
4. [assistant_runtime.go](./assistant_runtime.go)
5. [assistant_gmail.go](./assistant_gmail.go)
6. [assistant_forms.go](./assistant_forms.go)
7. [assistant_browser.go](./assistant_browser.go)
8. [assistant_test.go](./assistant_test.go)

## Local-Only Realities

- Some Gmail flows depend on local OAuth creds/token state
- Browser-computer tests may depend on an interactive signed-in browser
- WhatsApp native bridge requires local Node dependencies and pairing
- This repo may have untracked personal/local artifacts on a working machine; do not blindly commit those

## Things Not to Regress

- notebook capture UX
- `jot open` local viewer behavior
- assistant live progress updates
- Gmail send fallback to draft
- journal backup export
- direct-link form handling
- confirmation gates on send/delete/mutation actions

## Current Weak Spots

- cross-site form reliability is still beta
- messaging channels are not end-to-end finished
- exact-fact retrieval still needs more semantic polish
- public Gmail OAuth onboarding is intentionally not solved yet

## Practical Rule for Future Agents

Do not turn Jot into a dashboard product. Keep it local, fast, terminal-first, and capability-led.
