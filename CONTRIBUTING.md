# Contributing to jot

Thanks for contributing. jot is intentionally small and boring. Keep changes minimal, predictable, and easy to read.

## Prerequisites

- Go 1.22+
- Git

## Quick setup

```bash
git clone <your-fork>
cd jot
go build
```

Windows PowerShell:

```powershell
& "C:\Program Files\Go\bin\go.exe" build
```

## Running

```bash
./jot
./jot init
./jot list
./jot patterns
```

Windows PowerShell:

```powershell
& .\jot.exe
& .\jot.exe list
```

## Project layout

- `main.go` - All CLI behavior and file I/O.
- `go.mod` - Module definition.
- `Makefile` - Shortcuts for build/test/format tasks.

## Behavior at a glance

- Storage path: `~/.jot/journal.txt` (Windows: `%USERPROFILE%\.jot\journal.txt`).
- If the directory or file does not exist, it is created automatically.
- Entries are appended as `[YYYY-MM-DD HH:MM] <text>`.
- `jot` and `jot init` behave the same.
- `jot list` prints the entire file to stdout.
- `jot patterns` prints a fixed line.

## Design constraints (do not violate)

- No flags
- No config files
- No tags or folders
- One chronological file
- No interactivity beyond a single prompt

## Code notes

- `ensureJournal()` handles path resolution and lazy creation.
- `jotInit()` reads a single line and appends it with a timestamp.
- `jotList()` streams the journal to stdout.

## Tests

Run the full suite before you commit:

```bash
make test
# or
./scripts/test.sh
```

Windows PowerShell:

```powershell
make test
# or
.\scripts\test.ps1
```

`make test` runs the same checks as the scripts: `gofmt` verification and `go test ./...`.

If you want this enforced locally, install the optional git hook:

```bash
cp scripts/pre-commit .git/hooks/pre-commit
```

Windows PowerShell:

```powershell
Copy-Item scripts\pre-commit .git\hooks\pre-commit
```

If you add tests, keep them minimal and focused on behavior, not implementation details.

## Submitting changes

- Keep diffs small and focused.
- Update docs when behavior changes.
- Prefer clear, boring code over cleverness.
