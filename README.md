# jot
![open](https://github.com/user-attachments/assets/09757242-a56f-44e2-944b-d0b110f8705f)

<img width="1920" height="1080" alt="image" src="https://github.com/user-attachments/assets/9771f568-6bf2-45d0-a4bc-0eb80aee0848"/>

> **Open with Jot.
> View and Print: .json, .md, .xml and .pdf files locally using jot.**

**jot** is a terminal-first notebook and local document viewer.

Use it to capture raw thoughts fast, then use the same `jot open` flow to preview local PDFs, Markdown, JSON, and XML in a clean local viewer window.

No apps.
No dashboards.
Just one command for capture, and one command for opening the files you already have.

---

## quick start

```bash
curl -fsSL https://github.com/Intina47/jot/releases/latest/download/install.sh | sh
 or
brew install intina47/jot/jot-cli
or
npm install -g @intina47/jot
or (Windows)
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

Pick a file and jot opens it in the local viewer if it is a supported document type.

---

## what is jot?
<img width="792" height="710" alt="image" src="https://github.com/user-attachments/assets/8cf72867-bf6c-4941-8f5f-9b777b069594" />

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
 or
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

### capture with metadata

```bash
jot capture "note" --title "t" --tag foo --project "alpha"
```

If you omit the content, jot opens your editor and saves the result on exit:

```bash
jot capture --title "t"
```

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

Open one specific jot entry by id:

```bash
jot open dg0ftbuoqqdc-62
```

Open the native file picker:

```bash
jot open
```

Or open an existing local PDF in the jot viewer:

```bash
jot open "C:\Users\mamba\Downloads\paper.pdf"
```

Or open Markdown, JSON, or XML in the same jot viewer:

```bash
jot open ".\docs\plan.md"
jot open ".\data\sample.json"
jot open ".\feeds\config.xml"
```

Or browse the current folder in the jot viewer and switch between supported files:

```bash
jot open .
```

Or open any other existing local file with the system default app:

```bash
jot open ".\notes\todo.txt"
```

If the argument does not match a jot id and points to a local `.pdf`, `.md`, `.markdown`, `.json`, or `.xml`, jot starts a lightweight local viewer session and opens the file through jot's own viewer page. On machines with Edge, Chrome, Brave, or Chromium available, jot opens that viewer in a dedicated app-style window instead of a normal browser tab. Other files go through the normal system opener.

If the argument points to a directory such as `.`, jot opens a local folder browser that lists supported Markdown, JSON, XML, and PDF files in the current directory and previews them in place.

That means jot now works well as:

* a note capture tool
* a lightweight local PDF reader
* a Markdown previewer
* a JSON and XML inspection tool

On Windows, you can also add an Explorer context-menu entry:

```bash
jot integrate windows
```

That installs `Open with jot` for files under the current user. Remove it with:

```bash
jot integrate windows --remove
```

## tasks and image conversion

`jot` can now run lightweight terminal tasks without leaving the current folder.

If you already know what you want, use the direct command:

```bash
jot convert logo.png ico
jot convert logo.png svg
jot convert screenshot.png jpg
```

That writes the converted file next to the source image by default.

If you want the guided flow instead:

```bash
jot task
```

Pick `convert image`, choose the source file, then choose the target format.

Current image conversion support:

- inputs: `.png`, `.jpg`, `.jpeg`, `.gif`
- outputs: `.png`, `.jpg`, `.gif`, `.ico`, `.svg`

Notes:

- `.png` output preserves raster detail and alpha
- `.jpg` output is optimized for photos and screenshots and flattens transparency onto white
- `.gif` output is single-frame and palette-limited
- `.ico` output builds a multi-size favicon-style icon automatically
- `.svg` output wraps the source raster inside a standalone SVG file; it is not traced vector output

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

## templates

Create structured notes quickly with templates.

```bash
jot new --template daily
```

Add a name to create multiple notes from the same template in a day:

```bash
jot new --template meeting -n "Team Sync-Up"
```

Built-in templates:

```bash
jot templates
 or
jot list templates
```

Templates render a few variables:

* `{{date}}` → `YYYY-MM-DD`
* `{{time}}` → `HH:MM`
* `{{datetime}}` → `YYYY-MM-DD HH:MM`
* `{{repo}}` → current git repo name (empty if not in a repo)

### custom templates

Create a file in your config templates directory and use its filename (without extension) as the template name.

```
~/.config/jot/templates/standup.md
```

On Windows, this lives under `%AppData%\\jot\\templates`. If the config dir is not available, jot falls back to `~/.jot/templates`.

Then run:

```bash
jot new --template standup
```

---

## data & privacy

Your thoughts are yours.

* Stored locally by default
* Plain text (`~/.jot/journal.txt`)
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
