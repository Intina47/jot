
# jot

> **Keep one notebook for nonsense.
> That's where your real patterns hide.**

**jot** is a terminal-first notebook and local document viewer.

Capture a thought in one line, then use the same `jot open` flow to preview local PDFs, Markdown, JSON, and XML in a clean jot-owned viewer window.

## Install

```bash
npm install -g @intina47/jot
```

The npm package downloads the matching prebuilt binary from GitHub Releases during install.

Supported targets:

- macOS `x64`, `arm64`
- Linux `x64`
- Windows `x64`

## Quick start

Capture one thought:

```bash
jot
```

Or:

```bash
jot init
```

Capture a richer entry with metadata:

```bash
jot capture "Ship the help refresh" --title release --tag cli --project jot
```

If you omit the content, jot opens your editor:

```bash
jot capture --title "standup notes" --tag team
```

That handles capture.

When you want to inspect a local document:

```bash
jot open
```

Pick a file and jot opens supported document types in the jot viewer.

## Read back and open local docs

Browse the timeline:

```bash
jot list
```

Show the full terminal view without truncation:

```bash
jot list --full
```

Open one specific entry by id when a preview tells you to:

```bash
jot open dg0ftbuoqqdc-62
```

Open the native file picker:

```bash
jot open
```

Or open a local PDF in the jot viewer:

```bash
jot open "C:\Users\mamba\Downloads\paper.pdf"
```

Or open Markdown, JSON, or XML in the same jot viewer:

```bash
jot open ".\docs\plan.md"
jot open ".\data\sample.json"
jot open ".\feeds\config.xml"
```

If the argument is not a jot id and points to a local `.pdf`, `.md`, `.markdown`, `.json`, or `.xml`, jot starts a lightweight local viewer session and opens the file through jot's own viewer page. On machines with Edge, Chrome, Brave, or Chromium available, jot opens that viewer in a dedicated app-style window instead of a normal browser tab. Other existing files open with the system default app.

That means the same CLI now works well as:

- a note capture tool
- a lightweight local PDF reader
- a Markdown previewer
- a JSON and XML inspection tool

On Windows, you can add an Explorer context-menu entry for files:

```bash
jot integrate windows
```

Remove it with:

```bash
jot integrate windows --remove
```

## Templates

Create a dated note from a template:

```bash
jot new --template daily
```

Create multiple notes from the same template on the same day:

```bash
jot new --template meeting -n "Team Sync"
```

List available templates:

```bash
jot templates
```

## Help

The CLI now ships a fuller built-in help screen:

```bash
jot help
jot help capture
jot help list
```

## Data

- Journal entries are stored locally in `~/.jot/journal.jsonl`
- Template-created note files stay in your current working directory
- Nothing is uploaded by the CLI itself

## Notes for npm users

- The binary is fetched during `postinstall`
- Reinstalling the package downloads the binary for the current platform
- The package depends on the corresponding GitHub release assets already existing
