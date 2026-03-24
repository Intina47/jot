# jot

> **Keep one notebook for nonsense.
> That’s where your real patterns hide.**

**jot** is a terminal-first notebook and local document viewer.

Capture raw thoughts fast, then use the same `jot open` flow to preview local PDFs, Markdown, JSON, and XML in a clean jot-owned viewer window.

No apps.
No dashboards.
Just one command for capture, and one command for opening the files you already have.

---

## quick start

```bash
curl -fsSL https://github.com/Intina47/jot/releases/latest/download/install.sh | sh
# or
brew install intina47/jot/jot-cli
# or
npm install -g @intina47/jot
# or (Windows)
choco install jot
```

On macOS, Linux, and Git Bash on Windows, the `curl` installer uses `/usr/local/bin` when it can write there and falls back to a user bin directory otherwise.
In Windows PowerShell, use `choco install jot` or `npm install -g @intina47/jot` instead of piping into `sh`.
For Homebrew, use the fully qualified tap formula so Homebrew does not resolve to `homebrew-core/jot`.
For a machine-wide install, run:

```bash
curl -fsSL https://github.com/Intina47/jot/releases/latest/download/install.sh | sudo sh -s -- -b /usr/local/bin
```

Then, the moment a thought appears:

```bash
jot
```

Type.
Press enter.
Return to your work.

That’s the whole loop for capture.

And when you need to read something local:

```bash
jot open
```

Pick a file and jot opens supported document types in the local viewer.

---

## what is jot?

**jot** is two things that fit together:

1. A fast local notebook for half-formed thoughts.
2. A lightweight local document preview tool for files you want to inspect without jumping through heavy apps.

Not ideas.
Not tasks.
Not notes.

**Nonsense.**

The half-formed sentence.
The thing you thought at 01:43.
The idea you don’t respect *yet*.

Most tools ask you to be clear.
**jot lets you be early.**

Most file viewers ask you to open a full application.
**jot lets you just open the document.**

---

## why jot exists

Developers don’t lack tools.
They lack **a place where nothing has to make sense** and a fast way to inspect local documents without wrestling with file associations.

You open Notion when things are polished.
You open a doc when things are explainable.
You open Slack when things are urgent.

But where do you put the thought that feels stupid *until it isn’t*?

That’s what **jot** is for.

---

## the rule

There is only one rule:

> **Everything goes in the same notebook.**

No folders.
No tags.
No categories.

Time will do the sorting.

---

## usage

### capture a thought

```bash
jot
# or
jot init
```

You’ll see:

```
jot › what’s on your mind?
```

Type one line.
Press enter.
Exit silently.

No confirmation.
No formatting.
No dopamine tricks.

---

## reading back and opening local docs

```bash
jot list
```

You’ll see a simple timeline:

```
[2026-01-04 22:31] notion but in the terminal
[2026-01-06 09:12] onboarding tools assume users read
[2026-01-09 01:03] loneliness isn’t social, it’s unseen
```

This is not a feed.
It’s a mirror.

Template notes created in the current directory (like meeting, standup, or RFC notes) are included in the list output too.

Open one jot entry by id:

```bash
jot open dg0ftbuoqqdc-62
```

Open the native file picker:

```bash
jot open
```

Or open a local PDF, Markdown, JSON, or XML file:

```bash
jot open "C:\Users\mamba\Downloads\paper.pdf"
jot open ".\docs\plan.md"
jot open ".\data\sample.json"
jot open ".\feeds\config.xml"
```

If the argument is not a jot id and points to a local `.pdf`, `.md`, `.markdown`, `.json`, or `.xml`, jot opens it in jot's own lightweight viewer. Other files go through the normal system opener.

## tasks and image conversion

Use the direct command when you already know the job:

```bash
jot convert logo.png ico
jot convert logo.png svg
```

Or use the guided task flow:

```bash
jot task
```

Pick `convert image`, then choose the source image and target format.

Current image conversion support:

- inputs: `.png`, `.jpg`, `.jpeg`, `.gif`
- outputs: `.ico`, `.svg`

Notes:

- `.ico` output builds a multi-size favicon-style icon automatically
- `.svg` output wraps the source raster inside a standalone SVG file; it is not traced vector output

That means jot now works well as:

* a note capture tool
* a lightweight local PDF reader
* a Markdown previewer
* a JSON and XML inspection tool

---

## patterns

Eventually, curiosity wins.

```bash
jot patterns
```

For now, jot simply says:

> patterns are coming. keep noticing.

Later, it will reflect what you keep returning to — nothing more.

You may not like the answer.
That’s the point.

---

## what should I write?

If you’re unsure, start here:

* “this feels important but I don’t know why”
* “why does this annoy me every time?”
* “I keep circling this idea but avoiding it”
* “note to self: don’t forget how this felt”
* “this is probably nonsense”

Especially the last one.

---

## what jot is not

* ❌ a second brain
* ❌ a productivity system
* ❌ a knowledge base
* ❌ a markdown playground

Those come later — if they come at all.

jot lives **before structure**.

---

## philosophy

* Capture over clarity
* Friction is the enemy
* Chronology beats organization
* Patterns emerge, they are not forced

If it feels boring, it’s working.
If it feels quiet, you’re close.

---

## data & privacy

Your thoughts are yours.

* Stored locally by default
* Journal entries stored locally in `~/.jot/journal.jsonl`
* No lock-in
* Sync is optional, never assumed

If jot ever feels like a platform, uninstall it.

---

## who this is for

* developers who think in fragments
* founders with too many almost-ideas
* people who trust time more than tools

If you want to *optimize* your thinking, this isn’t for you.
If you want to **notice it**, welcome.

---

## uninstallation

Remove the binary however you installed it.
If you used the `curl` installer, remove `jot` from the install directory it chose, usually `~/.local/bin/jot` or `/usr/local/bin/jot`.

Your notebook stays.
Even if jot doesn’t.

---

## final note

Most ideas don’t fail because they’re bad.
They fail because they were embarrassed too early.

**jot** is where embarrassment goes to wait.

---
