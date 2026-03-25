# `jot task diff`

## Goal

Compare two local text files from the terminal and make the result easy to understand without leaving `jot`.

The feature should feel like a fast developer tool, not a browser diff site or a merge wizard. The direct command should be the habit, and `jot task` should remain the discovery layer for people who want a guided entry point.

## Primary Direct-Command UX

The direct path should be short and predictable:

```bash
jot diff before.txt after.txt
jot diff ./docs/spec.md ./docs/spec-new.md
jot diff ./src/app.old.ts ./src/app.ts --viewer
```

Default behavior:

- Compare two local paths as text.
- Print a compact terminal summary first.
- Show line counts, hunk counts, added lines, removed lines, and changed-file context when available.
- Keep the result local. No uploads, no browser handoff, no remote service.

The detailed viewer should be opt-in through a flag such as `--viewer`, so the command still works well in scripts and plain terminals.

## `jot task` Guided UX

`jot task diff` should keep the current `jot task` UI direction instead of introducing a new interaction model.

The guided flow should:

- Present `diff` as a numbered task in the existing task menu.
- Prompt for the left file and right file from the current directory, using the same terminal-first prompt style already used by other `jot task` flows.
- Offer optional context depth only if the user asks for advanced settings.
- Print the equivalent direct command after completion so the user can repeat it without the guided flow next time.

The guided flow should not add browser steps, upload steps, or a separate graphical diff editor.

## Inputs

Initial scope should be text files only.

Supported inputs:

- Plain text files
- Markdown
- Source code and scripts
- Config files such as JSON, YAML, TOML, XML, and `.env`
- Log and CSV files when they decode cleanly as text

Supported path forms:

- Two explicit file paths
- Relative or absolute local paths

Detection rules:

- Treat UTF-8 and ASCII as valid text.
- Normalize line endings before diffing so CRLF vs LF does not create fake changes.
- Reject binary files clearly when a file does not decode as text or contains binary data.

## Outputs

The command should produce two layers of output.

Terminal summary:

- The top-level summary should fit in the terminal and be useful on its own.
- Include added lines, removed lines, and hunk count.
- Include the compared file names.
- Keep unchanged regions collapsed in the summary.

Viewer rendering:

- The viewer should render the same diff locally in the current `jot` theme.
- Prefer a unified diff or a compact side-by-side diff that stays readable inside the existing viewer shell.
- Keep line numbers, additions, deletions, and unchanged context visible.
- Include a small summary header above the diff so the viewer is useful even without scrolling.

The viewer should not replace the terminal summary. It should complement it.

## Flags and Options

Recommended flags:

- `--viewer`
- `--summary-only`
- `--context <n>`
- `--ignore-whitespace`
- `--ignore-eol`
- `--word-diff`
- `--no-color`

Suggested defaults:

- `--context 3`
- `--viewer` off by default for scripts, optional for interactive sessions
- `--ignore-eol` on by default so line ending differences do not spam the diff

Behavior notes:

- `--ignore-whitespace` should suppress whitespace-only changes when requested.
- `--word-diff` should be limited to text lines and should not attempt to parse binary data.
- `--summary-only` should skip the viewer and print only the terminal summary.

## Error Handling

The command should fail clearly and early on:

- missing file arguments
- unsupported file types
- unreadable files
- nonexistent paths
- permission errors
- trying to diff the same path against itself
- binary files that are outside the text-diff scope

Error messages should say which path failed and why.

If one file is text and the other is not, the command should explain that the pair is not comparable in the text-diff task.

## Non-Goals

This task should not become a merge tool or a patch editor.

Out of scope for v1:

- browser-based diff tools
- upload flows
- three-way merge
- patch application
- conflict resolution
- binary diffing
- directory tree diffing
- git integration beyond local file comparison

The goal is a quick local text diff with a clear terminal summary and an optional local viewer.

## Implementation Notes

Keep the current `jot task` UI direction intact.

Practical shape:

- Add `diff` as another task entry in the existing task menu.
- Reuse the current terminal prompt and selection style for guided file picking.
- Use a deterministic line-based diff algorithm first, then layer optional word diff on top.
- Normalize line endings before comparison.
- Use the existing local viewer shell and theme so the diff render feels native to `jot`.
- Keep the renderer focused on text. Do not add unrelated UI patterns or a separate design language for diff.
- Print the direct command tip after guided completion so users graduate to `jot diff ...`.

Reasonable defaults:

- Treat text detection as content-based, not only extension-based.
- Prefer terminal summary output even when the viewer is available.
- Keep the viewer render compact enough to scan without extra navigation.

## Test Plan

Cover the feature with CLI and guided-flow tests.

Minimum cases:

- `jot diff left.txt right.txt` prints a useful terminal summary
- `jot diff left.txt right.txt --viewer` renders the diff viewer with the current theme
- `jot task diff` shows the guided flow and prints the direct command tip
- text files with different line endings do not produce fake changes
- whitespace-only changes are ignored when `--ignore-whitespace` is set
- same-file comparison fails with a clear error
- unreadable or missing files fail with a clear error
- binary files are rejected with a text-scope error
- the viewer render includes summary counts, line numbers, and added/removed markers

The tests should verify the terminal-first behavior and should not introduce any new UI model.
