# jot task palette

## Goal

`jot task palette` should extract a useful color palette from an image without leaving the terminal.

The task should help with the two common workflows developers and designers hit most often:

- turn an image into a short list of representative hex colors
- inspect the dominant colors in a screenshot, logo, or reference image before choosing a theme

The feature should stay local, deterministic, and terminal-first. It should not upload images or require a browser.

## Primary Direct-Command UX

The direct command should be easy to remember:

```bash
jot palette image.png
jot palette screenshot.jpg --count 5
jot palette logo.png --format hex
jot palette poster.png --format swatch
```

Recommended behavior:

- Default output should be a compact list of representative hex values.
- The command should analyze the image locally and return the most useful dominant colors.
- If the user does not specify a count, use a small default such as 5 or 8 colors.
- If the image contains transparency, the task should ignore fully transparent pixels by default unless the user explicitly wants them considered.

Suggested output style:

- hex list for quick copy/paste into CSS, docs, or config
- optional swatch rendering in the terminal for visual confirmation
- optional file output when the user explicitly asks for it

## `jot task` Guided UX

`jot task` should surface `palette` as a focused image-analysis task.

The guided path should:

1. Ask the user to choose an image from the current folder or provide a path directly.
2. Ask for the number of colors if the default is not sufficient.
3. Ask whether the user wants hex output or a swatch-style preview.
4. Run the extraction locally.
5. Show the equivalent direct command as the next-step hint.

The guided path should keep the current terminal UI and not replace it with a new visual flow.

## Inputs

- Local image files such as PNG, JPG, JPEG, GIF, and other formats already supported by the image pipeline
- Optional stdin input for future pipeline use if the implementation supports it
- Optional `--path` or positional path input

The task should treat the image as a local asset only.

## Outputs

- Representative hex colors
- Optional terminal swatches
- Optional structured output for scripting

The task should make it easy to copy colors into CSS, markdown notes, theme files, and design docs.

## Flags and Options

Recommended flags:

- `--count <n>` to choose the number of colors to extract
- `--format hex` to print hex values
- `--format swatch` to render terminal swatches
- `--format json` for script-friendly output
- `--ignore-alpha` to skip transparent pixels
- `--sort dominant|hue|luma` to control ordering
- `--stdin` to read image bytes from stdin if supported later

Recommended defaults:

- 5 colors by default
- hex output by default
- dominant-color ordering by default
- ignore fully transparent pixels by default

## Error Handling

The task should fail clearly when the image cannot be analyzed.

Expected errors:

- unreadable file
- unsupported image format
- empty image data
- invalid count or format flag
- output collision if a file output mode is added later

If the image is too noisy or too flat to produce a meaningful palette, the command should still return the best available dominant colors rather than failing unless the image is unreadable.

## Non-Goals

- Not a full color theory tool
- Not a browser-based palette generator
- Not a design-system editor
- Not a palette-to-theme generator in this task
- Not a remote image analysis service

## Implementation Notes

- Use local image decoding and clustering or quantization to derive representative colors.
- Prefer deterministic results so repeated runs on the same image produce the same palette.
- Keep the output compact and copyable by default.
- If swatch rendering is supported, make it terminal-safe and readable in the current UI style.
- Preserve the current `jot task` UI direction. This task should fit the same guided pattern as the other task ideas.
- Transparency handling should be explicit in the implementation and documented in the output.

## Test Plan

- Extract a palette from a simple logo image and verify the dominant colors are stable.
- Extract a palette from a screenshot and verify the output is compact and useful.
- Verify hex output is copyable and ordered as expected.
- Verify swatch output renders in the terminal.
- Verify count validation fails for invalid values.
- Verify unsupported or unreadable images fail clearly.
- Verify transparent pixels are ignored by default when requested or configured.
- Verify `jot task palette` leads the user through image selection and shows the direct command hint afterward.
