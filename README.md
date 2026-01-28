# jot
<img width="1536" height="1024" alt="jot" src="https://github.com/user-attachments/assets/19cf8f83-f82e-4fa3-a405-ec13612a0ea9" />


> **Keep one notebook for nonsense.
> That’s where your real patterns hide.**

**jot** is a terminal-first notebook for capturing raw thoughts — instantly, privately, without structure.

No apps.
No dashboards.
Just one command, one notebook, and time.

---

## quick start

```bash
brew install jot
# or
npm install -g @intina47/jot
# or (Windows)
choco install jot
```

Then, the moment a thought appears:

```bash
jot
```

Type.
Press enter.
Return to your work.

That’s the whole loop.

---

## what is jot?

**jot** is a notebook for thoughts that are not ready yet.

Not ideas.
Not tasks.
Not notes.

**Nonsense.**

The half-formed sentence.
The thing you thought at 01:43.
The idea you don’t respect *yet*.

Most tools ask you to be clear.
**jot lets you be early.**

---

## why jot exists

Developers don’t lack tools.
They lack **a place where nothing has to make sense**.

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

## reading back

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

Built-in templates:

```bash
jot templates
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

Your notebook stays.
Even if jot doesn’t.

---

## final note

Most ideas don’t fail because they’re bad.
They fail because they were embarrassed too early.

**jot** is where embarrassment goes to wait.

---
