# `jot task rename`

## Goal

Rename one file or many files from the terminal using predictable patterns, local previews, and explicit apply behavior.

The feature should feel safe enough for batch work and fast enough to become a normal local utility. The direct command should become the habit. `jot task rename` should be the guided entry point for users who want help constructing the rename pattern the first time.

## Primary Direct-Command UX

The direct path should stay short but explicit:

```bash
jot rename logo.jpeg --ext .jpg --apply
jot rename ./photos --replace " " "-" --case kebab --recursive --apply
jot rename "*.md" --template "{n:03}-{stem}" --dry-run
jot rename ./assets --prefix icon- --apply
```

The command should accept a single file path, a folder path, or a glob/pattern selector, then one rename strategy plus optional modifiers.

Default behavior:

- Build a rename plan first.
- Print the planned `old -> new` mapping.
- Do not modify anything unless the user passes `--apply`.
- Never overwrite an existing file by default.
- For folder and glob inputs, preserve each file in its current directory unless the task is explicitly extended later to move files.

The command should be safe by default because rename operations are easy to get wrong in batch mode.

## `jot task` Guided UX

`jot task rename` should preserve the current terminal-first task UI direction and reuse the same lightweight guided flow style as the existing task work.

The guided flow should:

- Let the user pick a source: single file, current-folder files, a folder, or a glob.
- Ask which rename strategy to use.
- Ask only for the minimum additional inputs required by that strategy.
- Show the preview plan before execution.
- Ask whether to apply the plan.
- Print the equivalent direct command after completion.

The guided flow should not introduce a new browser-like wizard, modal preview UI, or unrelated task UI redesign.

## Inputs

Supported selectors:

- A single file path.
- A folder path.
- A glob such as `./photos/*.jpeg`.
- Current-folder file selection through the guided task flow.

Initial rename scope:

- Files only.
- No directory renaming in v1.
- No cross-folder moves in v1.

Batch assumptions:

- Folder input should be non-recursive by default.
- `--recursive` should opt into walking subdirectories.
- Hidden files should only be included when the selector explicitly matches them or the user passes a later opt-in flag.

## Outputs

The output is a rename plan plus the applied rename results when execution is confirmed.

Dry-run output should show:

- source path or basename
- destination path or basename
- status per item: `rename`, `skip`, or `conflict`

Apply output should show:

- each successful rename
- each skipped item and why it was skipped
- each failed item and the error class
- one summary line with counts

The rename task should not silently change file extensions, letter case, or numbering unless the user explicitly chose a pattern that does so.

## Dry-Run Behavior

Dry-run should be the default.

Expected behavior:

- `jot rename ...` without `--apply` prints the planned rename map and exits with success if the plan is valid.
- `--dry-run` should be accepted as an explicit synonym for the default preview mode.
- `--apply` executes the plan after validation.
- If the plan contains conflicts, the preview should call them out clearly and the apply step should refuse to run unless the conflict policy explicitly permits a fallback mode.

This keeps rename safe in both direct-command and guided usage.

## Pattern Types

V1 should support a small set of high-value rename patterns.

### 1. Replace

Replace one substring with another:

```bash
jot rename "*.jpeg" --replace ".jpeg" ".jpg" --apply
jot rename ./docs --replace " " "-" --apply
```

Rules:

- Replace should operate on the stem by default.
- `--replace-ext` can opt into replacing inside the extension or changing the extension with the same mechanism if needed later.
- Empty replacement text should be allowed.

### 2. Prefix

Add text before the filename stem:

```bash
jot rename "*.png" --prefix icon- --apply
```

Rules:

- Preserve the original extension unless another flag changes it.

### 3. Suffix

Add text after the filename stem:

```bash
jot rename "*.md" --suffix -draft --apply
```

Rules:

- Preserve the original extension unless another flag changes it.

### 4. Extension Change

Change the extension only:

```bash
jot rename "*.jpeg" --ext .jpg --apply
```

Rules:

- This is naming-only. It does not convert file contents.
- The command should warn when the extension change is likely misleading, such as renaming a `.png` file to `.jpg` without conversion.

### 5. Case Transform

Normalize the filename stem:

```bash
jot rename ./snaps --case kebab --apply
jot rename "*.txt" --case snake --apply
```

Supported case modes:

- `lower`
- `upper`
- `title`
- `kebab`
- `snake`

Rules:

- Case transforms should operate on the stem by default.
- Extensions should stay lowercase by default unless a dedicated flag is added later.

### 6. Template Rename

Generate the new name from explicit tokens:

```bash
jot rename "*.png" --template "{stem|kebab}-{n:03}{ext}" --apply
```

Initial token set:

- `{stem}`: original filename without extension
- `{ext}`: original extension including the leading dot
- `{n}`: sequence number starting at 1
- `{n:03}`: zero-padded sequence width
- `{parent}`: immediate parent directory name

Optional simple filters:

- `{stem|lower}`
- `{stem|upper}`
- `{stem|kebab}`
- `{stem|snake}`

Rules:

- Template mode is the most flexible path and should remain text-based rather than turning into a mini scripting language.
- Unknown tokens should fail validation before apply.

## Collision Handling

Collision handling has to be explicit because rename operations can destroy work if done casually.

Default behavior:

- Validate the full plan before any rename starts.
- Abort apply if any destination path already exists outside the rename set.
- Abort apply if two planned renames target the same final path.

Recommended conflict policies:

- `abort` (default): stop and report all conflicts.
- `skip`: skip only conflicting items, apply the rest.
- `suffix`: append a stable numeric suffix such as `-2`, `-3`, and continue.

Do not support `overwrite` in v1.

Additional safety rule:

- Swaps such as `a.txt -> b.txt` and `b.txt -> a.txt` should be handled through a temporary staging rename, not by naive in-place ordering.

## Flags and Options

Recommended flags:

- `--replace <from> <to>`
- `--prefix <text>`
- `--suffix <text>`
- `--ext <extension>`
- `--case lower|upper|title|kebab|snake`
- `--template <pattern>`
- `--recursive`
- `--apply`
- `--dry-run`
- `--on-conflict abort|skip|suffix`
- `--quiet`

Validation rules:

- Exactly one primary rename strategy should be required in v1: `--replace`, `--prefix`, `--suffix`, `--ext`, `--case`, or `--template`.
- Modifiers such as `--recursive`, `--apply`, and `--on-conflict` may be combined with any strategy.
- If no strategy is provided, the command should fail with a useful help hint.

## Error Handling

The command should fail clearly and early on:

- nonexistent files or folders
- empty rename strategy
- malformed template tokens
- invalid case mode
- invalid extension text
- unreadable files
- permission errors
- collisions in the final plan

Batch behavior should be predictable:

- Preview mode should list all detected conflicts.
- Apply mode should preflight the full plan before renaming anything when `--on-conflict abort`.
- `--on-conflict skip` may continue past conflicting items, but should still return a non-zero exit code if any item failed or was skipped due to conflict.

Error messages should name the problem, the affected path, and the recovery step when possible.

## Non-Goals

This feature should not become a file organizer or a scripting engine.

Out of scope for v1:

- moving files between directories
- renaming directories
- content-aware renaming
- regex-based replace
- EXIF/date-based rename logic
- git-aware renames
- undo history across sessions
- filename linting for every language or platform
- shell-expression evaluation in templates

The point is safe local renaming, not a general automation runtime.

## Implementation Notes

Suggested shape:

- Keep rename planning separate from rename execution.
- Build a normalized list of input files first.
- Compute the full rename plan next.
- Validate collisions and illegal outputs before any rename happens.
- Execute with a staging strategy for swaps and cycles.

Practical defaults:

- Preview first, apply explicitly.
- Non-recursive folder behavior by default.
- Preserve extension unless the user asks to change it.
- Show basenames in compact mode and full paths when required for disambiguation.

Execution notes:

- Use a stable sort order before applying sequence-based templates so numbering is reproducible.
- Use temporary names in the same directory when resolving swaps.
- Normalize extension handling so `jpg` becomes `.jpg`.
- Treat case-only renames carefully on case-insensitive filesystems.

Guided task notes:

- Keep the current `jot task` shell.
- Present rename strategy choices as a short numbered list.
- For dry-run in guided mode, show the preview and then ask for confirmation to apply.
- After completion, print the exact direct command that would reproduce the operation.

## Test Plan

Cover both direct-command and guided task behavior.

Minimum cases:

- single-file prefix rename in preview mode prints the expected mapping without changing the file
- single-file prefix rename with `--apply` renames the file
- folder rename without `--recursive` leaves nested files untouched
- folder rename with `--recursive` includes nested files
- replace strategy renames all matching files
- extension-only rename changes the filename suffix without touching file contents
- case transform produces stable kebab and snake outputs
- template rename with `{n:03}` produces stable sequence numbering
- duplicate destinations are detected during preflight
- `--on-conflict skip` skips conflicting items and still applies safe items
- swap renames use staging and complete successfully
- case-only renames behave correctly on case-insensitive platforms
- invalid templates fail before apply
- `jot task rename` shows the guided flow, preview plan, confirmation step, and the direct command tip after completion

The tests should verify the current terminal-first UI direction and should not introduce a different interaction model.
