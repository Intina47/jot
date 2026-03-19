
# jot

> **Keep one notebook for nonsense.
> That's where your real patterns hide.**

**jot** is a terminal-first notebook for capturing raw thoughts, quick notes, and lightweight files without leaving the shell.

## Install

```bash
npm install -g @intina47/jot
```

The npm package downloads the matching prebuilt binary from GitHub Releases during install.

Supported targets:

- macOS `x64`, `arm64`
- Linux `x64`, `arm64`
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

## Read back

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

Or open a local PDF in the browser:

```bash
jot open "C:\Users\mamba\Downloads\paper.pdf"
```

If the argument is not a jot id and points to a local `.pdf`, jot opens it through a temporary local browser URL instead of relying on the system `.pdf` file association. Other existing files open with the system default app.

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
