# `jot task resize`

## Goal

Resize one image or many images from the terminal without leaving the current folder or switching tools.

The feature should feel like a local utility, not a wizard. The direct command should be the habit, and `jot task` should be the guided on-ramp for users who want to discover the capability first.

## Primary Direct-Command UX

The direct path should be short and predictable:

```bash
jot resize ./photos 1280x720
jot resize logo.png 512x512
jot resize ./images 1600x900 --out-dir ./resized
```

The command should accept either a single file, a glob, or a folder as input, then a target size as `WIDTHxHEIGHT`.

Default behavior:

- Preserve aspect ratio by default using a `fit` resize into the requested box.
- Write resized copies next to the originals for a single file.
- Write batch output into a dedicated output folder for folder and glob inputs.
- Never overwrite originals unless the user asks for it explicitly.

The command should print a compact summary of what it produced, including the output path(s), final dimensions, and any files that were skipped.

## `jot task` Guided UX

`jot task resize` should keep the current task UI style and use it only as a discovery flow.

The guided flow should:

- Let the user choose a source file, folder, or glob from the current directory.
- Ask for the target size in `WIDTHxHEIGHT` form.
- Offer the resize mode as a small choice: `fit`, `fill`, or `stretch`.
- Show the direct command that would reproduce the action after completion.

The guided flow should not add browser-like steps, upload steps, or any UI that departs from the current `jot task` direction.

## Inputs

Initial input scope:

- `.png`
- `.jpg`
- `.jpeg`
- `.gif`

Supported selectors:

- A single file path.
- A folder path, processed non-recursively by default and recursively with `--recursive`.
- A glob such as `./photos/*.jpg`.

Assumptions:

- JPEG orientation should be respected when present.
- Animated GIF handling should be treated as a separate concern if it is supported later.

## Outputs

Default output rules:

- Single file input: write a sibling file named with a size suffix, for example `logo-512x512.png`.
- Folder or glob input: write into an output directory such as `./resized/`, preserving relative paths.
- Output format should default to the same format as the input.

Output naming should be stable and obvious. The file name should make the resize dimensions visible without forcing the user to inspect metadata.

When the user opts into in-place output, the command may overwrite the original file only after a clear confirmation or an explicit `--force`.

## Flags and Options

Recommended flags:

- `--mode fit|fill|stretch`
- `--out-dir <path>`
- `--in-place`
- `--force`
- `--recursive`
- `--dry-run`
- `--quiet`

Behavior of the main modes:

- `fit`: preserve aspect ratio and fit inside the target box.
- `fill`: preserve aspect ratio, crop to exact dimensions.
- `stretch`: ignore aspect ratio and force the exact target dimensions.

The default should be `fit`.

## Error Handling

The command should fail clearly and early on:

- missing or malformed sizes
- unsupported image formats
- unreadable files
- nonexistent paths
- permission errors
- output path collisions when overwrite is not allowed

Batch mode should be resilient:

- continue processing other files when one file fails
- summarize failures at the end
- return a non-zero exit code if any file failed

If a target dimension is invalid, the error message should show the expected format and a valid example.

## Non-Goals

This task should not try to become a full image editor.

Out of scope for v1:

- browser-based upload flows
- freeform cropping tools
- rotation and perspective correction
- animated GIF editing
- watermarking
- color adjustment
- image tracing
- deep metadata editing

The goal is a fast resize utility, not an editor replacement.

## Implementation Notes

Prefer an implementation that stays local and deterministic.

Suggested shape:

- Use the existing `jot task` UI shell and add resize as another task entry.
- Use high-quality resampling for downscaling so resized images do not look soft or aliased.
- Preserve EXIF orientation before resizing when the source format supports it.
- Keep the output pipeline streaming and file-based so batch jobs do not require loading everything into memory at once.
- Keep the resize logic separate from conversion logic so the task stays easy to test and extend.

Practical defaults:

- `fit` should be the default resize mode.
- The command should not prompt for a size preset unless the user omits one.
- When the user provides a folder, preserve directory structure in the output.

## Test Plan

Cover the feature with both CLI and task-flow tests.

Minimum cases:

- single-file `fit` resize writes the expected sibling output
- folder resize writes into the output directory and preserves structure
- glob input expands and processes all matching files
- `fill` produces exact dimensions
- `stretch` forces exact dimensions
- malformed size strings fail with a useful error
- unsupported input types fail cleanly
- overwrite protection blocks accidental replacement
- `--dry-run` reports planned outputs without writing files
- `jot task resize` renders the guided flow and prints the direct command tip after completion

The tests should verify the current terminal-first UX and should not introduce any new UI model.
