# Jot Assistant

`jot assistant` adds a terminal-first assistant runtime to Jot. V1 is designed around Gmail, with provider and capability interfaces so Calendar, GitHub, filesystem tools, and more model backends can be added without rewriting the command surface.

Gmail and Google Calendar integration currently target local and private use. Public one-click Gmail onboarding for general users is deferred until Jot has its own verified Google OAuth app and verified application domain.

## Prerequisites

- Install and run Ollama locally.
- Pull a local model before first use:

```bash
ollama pull llama3.2
```

- Build Jot as usual:

```bash
go build -ldflags="-H windowsgui" -o jot.exe .
go build -o jot .
```

## First Run

The easiest path is now the guided onboarding flow:

```bash
jot assistant --onboarding
```

You can also just run:

```bash
jot assistant
```

If provider setup or Gmail auth is incomplete, Jot will walk you through:

- choosing the provider
- choosing the model
- entering the provider API key if needed
- testing the model connection
- connecting Gmail in the browser
- connecting the browser computer with a dedicated local browser profile
- returning to the terminal for a quick recent-email summary

For Gmail, the onboarding flow currently expects Google Desktop OAuth client credentials under your control. This is intended for private/local use until Jot's public OAuth setup is ready. You can either paste the client id and client secret during onboarding, or preconfigure:

```bash
export JOT_GMAIL_CLIENT_ID=your-client-id
export JOT_GMAIL_CLIENT_SECRET=your-client-secret
```

Or save the same values in your Jot config directory as `gmail_credentials.json`.

## Browser Computer

The browser computer uses a dedicated local Chrome or Edge profile so Jot can help with authenticated browser tasks such as Google Forms.

The recommended setup is built into onboarding, but you can also manage it directly:

```bash
jot assistant auth browser
jot assistant browser status
jot assistant browser connect
jot assistant browser disconnect
```

When you connect the browser computer, Jot opens a dedicated browser window and asks you to sign in yourself. Jot does not ask for your Google password. The signed-in session remains local to your machine inside the Jot browser profile.

Once setup is complete:

```bash
jot assistant "summarize unread emails from today"
```

You can also start an interactive session with:

```bash
jot assistant
```

## Provider Configuration

Ollama is the provider available in the current build. You can still override the model directly:

```bash
export JOT_ASSISTANT_MODEL=llama3.2
```

For Ollama Cloud, also set:

```bash
export OLLAMA_API_KEY=your-api-key
```

## Web UI

The assistant can launch a local viewer using the same app-window pattern as `jot open`:

```bash
jot assistant --ui
```

The viewer serves a local page on `http://127.0.0.1:<port>` and opens it in a Chromium app window when available.

## JSON Output

For scripting and pipelines, request structured output:

```bash
jot assistant --format json "extract tasks from unread emails" | jq .
```

## Status and Raw Capability Commands

Examples:

```bash
jot assistant status
jot assistant gmail search "is:unread newer_than:1d"
jot assistant gmail summarize --unread --today
jot assistant gmail attachments --last 30d --save ./invoices
```
