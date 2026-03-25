# jot task qr

## Goal

`jot task qr` should generate a QR code from text or a URL without leaving the terminal.

The task should be useful in the two cases developers hit most often:

- turn a URL into a shareable QR code image
- turn any short text, token, or local note into a scannable QR code

The UX should stay local-first, predictable, and fast. No browser detours, no pastebin flow, no remote encoding service.

## Primary Direct-Command UX

The direct command should be simple enough to remember after one use:

```bash
jot qr https://example.com
jot qr "ssh://git@example.com/repo"
jot qr "hello world" --svg
jot qr --text "hello world" --ascii
```

Recommended behavior:

- Default output should be a PNG file when writing to disk.
- If the user passes `--ascii`, print a terminal-friendly QR preview instead of writing an image.
- If the user passes `--svg`, write a scalable SVG QR image.
- If the input is short and no output path is provided, write the file next to the current working directory using a predictable name such as `qr.png` or `qr.svg`.

Suggested naming:

- `qr.png` for the default raster output
- `qr.svg` for scalable output
- `qr-<slug>.png` when multiple QR codes or explicit naming is needed later

## `jot task` Guided UX

`jot task` should present `qr` as a focused task entry.

The guided path should:

1. Ask whether the user wants to encode a URL or arbitrary text.
2. Let the user paste or type the payload directly in the terminal.
3. Ask for the output format if it is not already obvious:
   - `png`
   - `svg`
   - `ascii`
4. Ask for output location if the user chooses a file-based format.
5. Generate the QR code.
6. Show the equivalent direct command as the next-step hint.

The guided path should teach the direct command, not replace it.

## Inputs

- URLs
- Short text strings
- Piped stdin for scripted generation
- Optional `--text` input for inline use

The task should treat the payload as opaque text. It should not try to validate or rewrite URLs beyond basic emptiness checks.

## Outputs

- PNG QR code for common shareable file output
- SVG QR code for scalable output
- ASCII QR rendering in the terminal for quick checks or copy/paste workflows

Default QR parameters should produce codes that are easy to scan in normal use and still small enough to be practical.

## Flags and Options

Recommended flags:

- `--text <value>` to pass inline content
- `--stdin` to read the payload from stdin
- `--out <path>` to choose the output file
- `--force` to allow overwriting an existing output file
- `--png` to force PNG output
- `--svg` to force SVG output
- `--ascii` to print a terminal rendering instead of writing a file
- `--size <px>` for raster output size
- `--margin <n>` for quiet zone control
- `--level <L|M|Q|H>` for QR error correction level

Recommended defaults:

- `png` as the default file output
- medium or high error correction as the default if the implementation supports it without making the QR unnecessarily dense
- a quiet zone large enough for reliable scanning

## Error Handling

The task should fail clearly for invalid or unusable input.

Expected errors:

- empty payload
- unreadable stdin
- invalid output path
- overwrite collision without permission
- unsupported or conflicting flags

If the payload is very long, the command should either:

- generate a denser QR code if it still fits within library limits, or
- fail with a clear message that the payload is too large to encode safely

The command should not silently truncate the input.

## Non-Goals

- Not a remote QR generator
- Not a QR scanner or decoder
- Not a general barcode suite
- Not a browser canvas tool
- Not a multi-page design surface for posters or branded assets

## Implementation Notes

- Use a local QR generation library rather than hand-rolling the matrix.
- Keep the default output path and naming predictable so the command feels immediate.
- Prefer image outputs that work well for sharing in docs, chat, and issue comments.
- Make the ASCII output legible enough for terminal previews, but do not treat it as the canonical artifact.
- Preserve the current `jot task` UI direction. This feature should fit the same discovery flow and terminal styling already in place.
- Keep URL handling simple. The task should encode exactly what the user typed.
- If the implementation supports both file and stdout output, make the file path the default for image formats and stdout the default for `--ascii`.

## Test Plan

- Generate a QR code from a short URL and verify the image file is created.
- Generate a QR code from plain text and verify the payload is preserved.
- Verify SVG output is scalable and contains the encoded payload.
- Verify ASCII output renders in the terminal.
- Verify stdin input works.
- Verify empty input fails clearly.
- Verify overwrite protection works.
- Verify `jot task qr` leads the user through payload and output selection and shows the direct command hint afterward.
