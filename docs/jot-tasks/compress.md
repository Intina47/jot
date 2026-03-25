# `jot task compress`

## Goal

Create archives from the terminal quickly and predictably.

The task should cover the common developer habit of packaging a folder or a set of files into a zip or tar archive without leaving the current directory or switching to a GUI tool.

The direct command should be the habit after the first use. `jot task` should remain the discovery layer for users who want a guided flow.

## Primary Direct-Command UX

The direct path should be short and easy to remember:

```bash
jot compress ./project zip
jot compress ./project tar
jot compress ./project tar.gz
jot compress ./release/*.txt zip
```

The command should accept a file, a folder, or a glob as input, then an archive format.

Default behavior:

- Use the input name as the archive base name when no explicit output name is provided.
- Write the archive next to the source by default.
- Preserve directory structure inside the archive.
- Never overwrite an existing archive unless the user explicitly asks for it.

The command should print a short summary with the archive path, format, and entry count.

## `jot task` Guided UX

`jot task compress` should keep the current `jot task` UI direction and only act as a guided front door.

The guided flow should:

- Let the user choose a source file, folder, or glob from the current directory.
- Ask for the archive format.
- Ask for an optional archive name only if the default name would be ambiguous or conflicting.
- Show the equivalent direct command after the archive is created.

The flow should stay terminal-first and should not introduce browser-style uploads, previews, or unrelated UI changes.

## Inputs

Initial input scope:

- A single file path.
- A folder path.
- A glob such as `./assets/*.png`.

Recommended archive source behavior:

- Folder inputs should archive the folder contents, not the parent directory unless the user opts into that explicitly.
- Glob inputs should expand before archiving.
- Multiple explicit file arguments should be archived into one file in the order given.

The task should work with normal local files only. It does not need to reach into remote sources.

## Outputs

Supported archive outputs for v1:

- `.zip`
- `.tar`
- `.tar.gz`

Default output rules:

- Single folder input: create `<folder-name>.<format>` next to the folder.
- Single file input: create `<file-name>.<format>` next to the file.
- Glob or multi-file input: create an archive named after the provided output name or a stable default like `archive.<format>`.

The output name should be obvious and predictable. Users should not have to guess where the archive landed.

Archive contents should preserve relative paths so the archive can be unpacked cleanly elsewhere.

## Flags and Options

Recommended flags:

- `--format zip|tar|tar.gz`
- `--out <path>`
- `--name <name>`
- `--force`
- `--dry-run`
- `--quiet`
- `--include-hidden`
- `--exclude <glob>`

Behavior notes:

- `--format` should be the primary selector for the archive type.
- `--out` should allow an explicit archive file path.
- `--name` should set the base archive name while keeping the chosen format suffix.
- `--force` should allow overwrite of an existing archive.
- `--exclude` should be repeatable for common cleanup patterns like `node_modules`, build folders, or caches.

Recommended defaults:

- `zip` as the default archive format for the broadest compatibility.
- hidden files excluded unless the user opts in.
- deterministic file ordering inside the archive when possible.

## Error Handling

The command should fail clearly on:

- missing input paths
- unsupported archive formats
- unreadable files or folders
- permission problems
- path collisions when overwrite is not allowed
- invalid glob patterns

Batch and multi-file behavior should be resilient:

- if a source file cannot be read, fail that archive creation with a clear message
- if one glob match is unreadable, report it in the summary
- return a non-zero exit code when any archive creation fails

If the chosen archive name already exists, the command should refuse to overwrite it unless `--force` is set.

## Non-Goals

This task should not become a general backup system or a cloud sync feature.

Out of scope for v1:

- encrypted archives
- password prompts
- split archives
- remote upload or download
- incremental backup snapshots
- archive repair tooling
- browser-based file selection

The goal is fast local archive creation, not a full backup suite.

## Implementation Notes

Prefer a local and deterministic implementation.

Suggested shape:

- Use the existing `jot task` UI shell and add compress as another task entry.
- Keep the archive creation logic separate from the task selection flow.
- Preserve directory structure inside the archive so extraction is predictable.
- Stream file contents into the archive instead of reading everything into memory at once.
- Use stable ordering for archived entries so repeated runs are easier to diff and test.

Practical defaults:

- `zip` should be the default format.
- folder and glob selections should default to a stable archive name derived from the input.
- hidden files should be opt-in rather than included silently.

If archive format support needs platform-specific behavior, the feature should still present the same CLI surface across platforms.

## Test Plan

Cover both direct CLI behavior and the guided task flow.

Minimum cases:

- folder input creates a zip archive with the expected base name
- file input creates an archive with the expected base name
- glob input expands and archives the matched files
- explicit multi-file input creates one archive in the given order
- `tar` and `tar.gz` outputs are created with the expected suffixes
- `--out` writes to the requested path
- `--name` overrides the archive base name
- `--force` allows overwriting an existing archive
- overwrite protection blocks accidental replacement
- `--dry-run` reports the planned archive without writing it
- invalid archive formats fail with a useful error
- `jot task compress` renders the guided flow and prints the direct-command tip after completion

The tests should validate that the task stays terminal-first and that no new UI model is introduced.
