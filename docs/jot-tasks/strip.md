# `jot task strip`

## Goal

Strip EXIF and related metadata from local image files without leaving the terminal.

The feature should feel like a quick privacy and cleanup utility, not a photo editor. The direct command should be the habit, and `jot task` should remain the discovery layer for users who want a guided entry point.

## Primary Direct-Command UX

The direct path should be short and predictable:

```bash
jot strip photo.jpg
jot strip ./images/logo.png
jot strip ./photos --recursive
jot strip ./images --out-dir ./stripped
```

Default behavior:

- Strip non-visual metadata from image files.
- Write a sibling output file by default so the original stays intact.
- Use a clear suffix such as `-stripped` before the extension.
- Print a compact terminal summary of what changed.

The command should be safe by default. In-place mutation should require an explicit flag.

## `jot task` Guided UX

`jot task strip` should keep the current `jot task` UI direction instead of introducing a new interaction model.

The guided flow should:

- Present `strip` as a numbered task in the existing task menu.
- Prompt for the source file, folder, or glob from the current directory using the same terminal-first prompt style already used by other `jot task` flows.
- Ask whether to write sibling output, use an output directory, or strip in place only if the user opts into advanced settings.
- Show the equivalent direct command after completion so the user can repeat it without the guided flow next time.

The guided flow should not add browser steps, upload steps, or any UI that departs from the current `jot task` direction.

## Inputs

Initial scope should be common local image formats that carry metadata:

- `.jpg`
- `.jpeg`
- `.png`
- `.webp`
- `.tif`
- `.tiff`
- `.gif` when metadata is present

Supported path forms:

- A single file path
- A folder path, processed recursively when requested
- A glob such as `./photos/*.jpg`

Detection rules:

- Treat files as images only when the decoder can open them successfully.
- Reject non-image files clearly.
- Preserve pixel data and animation frames where the source format supports animation.

## Outputs

Default output rules:

- Single file input: write a sibling file named with a `-stripped` suffix.
- Folder or glob input: write into a dedicated output directory such as `./stripped/`, preserving relative paths.
- If the user chooses in-place mode, overwrite only with an explicit opt-in flag.

The command should print a compact summary that includes:

- the input path or count of inputs processed
- the output path or output directory
- whether metadata was removed
- any files skipped because they were already clean, unreadable, or unsupported

The output should stay terminal-first. A viewer is not required for this task because the result is a file transformation, not a document inspection flow.

## Flags and Options

Recommended flags:

- `--out-dir <path>`
- `--in-place`
- `--force`
- `--recursive`
- `--dry-run`
- `--quiet`
- `--keep-icc`
- `--preserve-color-profile`

Default behavior:

- `--in-place` off by default
- `--force` off by default
- `--recursive` off unless the input is a directory and the user requests it
- preserve color profiles unless the user explicitly asks for a full strip

Behavior notes:

- `--keep-icc` or `--preserve-color-profile` should keep the embedded color profile while removing EXIF, XMP, IPTC, comments, GPS, thumbnails, and similar metadata.
- A full-strip mode may remove color profiles too, but that should be explicit rather than the default.
- `--dry-run` should report the planned output paths without writing files.

## Error Handling

The command should fail clearly and early on:

- missing inputs
- unsupported file types
- unreadable files
- nonexistent paths
- permission errors
- output path collisions when overwrite is not allowed
- attempts to strip the same file in place without explicit permission

If a file is already stripped, the command should say so instead of failing noisily.

Batch mode should be resilient:

- continue processing other files when one file fails
- summarize failures at the end
- return a non-zero exit code if any file failed

Error messages should identify the path that failed and the reason.

## Non-Goals

This task should not become a photo editor or an image optimizer.

Out of scope for v1:

- resizing
- cropping
- compression tuning
- color correction
- format conversion
- browser-based upload flows
- remote processing
- OCR
- image tracing
- watermarking

The goal is metadata stripping, not general image editing.

## Implementation Notes

Keep the current `jot task` UI direction intact.

Practical shape:

- Add `strip` as another task entry in the existing task menu.
- Reuse the current terminal prompt and selection style for guided file picking.
- Implement stripping as a local file rewrite pipeline that decodes and re-encodes images with metadata removed.
- Preserve the image format by default so the command does not surprise users.
- Preserve animation for GIF/WebP sources where possible, even if the metadata is removed.
- Keep the output pipeline file-based and deterministic so it is easy to test.
- Do not add unrelated UI patterns or a different interaction model for the guided flow.

Metadata scope:

- Strip EXIF data.
- Strip XMP and IPTC metadata.
- Strip comment blocks and embedded thumbnails.
- Preserve the visual image content.
- Preserve color profiles by default unless the user explicitly asks for a full strip.

Reasonable defaults:

- Sibling output is safer than in-place mutation.
- `jot task` should start simple and only ask for advanced options if the user asks for them.
- The direct command should be the remembered path after the first guided run.

## Test Plan

Cover the feature with both CLI and task-flow tests.

Minimum cases:

- `jot strip photo.jpg` writes a sibling `-stripped` output file
- `jot strip ./images --recursive` processes nested image files
- `jot strip ./images --out-dir ./stripped` writes to the requested output directory
- `jot strip photo.jpg --in-place --force` rewrites the source file when explicitly allowed
- `--dry-run` reports planned outputs without writing files
- unsupported inputs fail with a clear error
- unreadable or missing files fail with a clear error
- already-stripped files report a clean no-op result
- `jot task strip` renders the guided flow and prints the direct command tip after completion
- metadata stripping removes EXIF/XMP/IPTC content while preserving the image pixels

The tests should verify the current terminal-first UX and should not introduce any new UI model.
