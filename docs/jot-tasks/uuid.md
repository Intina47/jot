# `jot task uuid`

## Goal

Generate UUIDs, nanoids, and other random identifiers quickly from the terminal.

The task should serve the common developer need for one-off IDs, test data, fixture generation, and copyable random strings without leaving the current shell.

The direct command should become the habit after first use. `jot task` should stay the discovery layer for users who want a guided path.

## Primary Direct-Command UX

The direct path should be short and easy to remember:

```bash
jot uuid
jot uuid --type uuid
jot uuid --type nanoid
jot uuid --count 10
jot uuid --type string --length 24
```

The command should support generating a single identifier or a batch of identifiers.

Default behavior:

- Generate one UUID v4 by default.
- Print one value per line.
- Copying from the terminal should be easy, with no extra decoration unless the user asks for it.

The command should emit only the values by default so it is easy to pipe into other commands.

## `jot task` Guided UX

`jot task uuid` should keep the current `jot task` UI direction and only act as a guided entry point.

The guided flow should:

- Ask which identifier type the user wants.
- Offer a small choice set such as `uuid`, `nanoid`, and `random string`.
- Ask for the count if the user wants more than one value.
- Ask for length only when the chosen type needs it.
- Show the direct command that reproduces the action after generation.

The flow should stay terminal-first and should not introduce unrelated UI, browser detours, or copy-heavy screens.

## Inputs

This task is mostly parameter-driven rather than file-driven.

Supported inputs:

- No input, for the default one-value generation path.
- Optional type selection.
- Optional count.
- Optional length for random strings or nanoids when the user wants a custom size.

If a seed is supported later, it should be explicit and opt-in rather than implied.

## Outputs

Supported output types for v1:

- UUID v4
- Nanoid
- Random alphanumeric string

Default output rules:

- One value per line.
- No surrounding quotes.
- No extra labels in machine-friendly mode.

If the user requests a batch, the output should remain line-oriented so it can be piped or redirected easily.

## Flags and Options

Recommended flags:

- `--type uuid|nanoid|string`
- `--count <n>`
- `--length <n>`
- `--alphabet <chars>`
- `--upper`
- `--lower`
- `--clipboard`
- `--quiet`

Behavior notes:

- `--type` should pick the identifier family.
- `--count` should generate multiple values.
- `--length` should apply to nanoids and random strings when relevant.
- `--alphabet` should allow custom random-string character sets.
- `--clipboard` can copy the single generated value when the user wants a quick paste flow.

Recommended defaults:

- `uuid` as the default type.
- a reasonable nanoid length if `nanoid` is chosen without an explicit length.
- lowercase output for string mode unless the user asks otherwise.

## Error Handling

The command should fail clearly on:

- invalid counts
- invalid lengths
- unsupported identifier types
- empty alphabets
- non-numeric length or count values

Batch behavior should be predictable:

- if the user asks for `count = 0`, return a clear validation error
- if one generation path fails, do not emit partial garbage without warning

If the user requests clipboard output in a non-interactive context, the command should fail gracefully or skip clipboard behavior with a clear message.

## Non-Goals

This task should not become a full cryptography library or a secret manager.

Out of scope for v1:

- cryptographic keypair generation
- password generation with policy enforcement
- persistent ID registries
- sortable ULIDs unless added as a separate explicit type later
- browser-based flows
- network-backed uniqueness checks

The goal is fast local identifier generation, not identity infrastructure.

## Implementation Notes

Prefer a local, deterministic-by-interface implementation with strong randomness under the hood.

Suggested shape:

- Use the existing `jot task` UI shell and add uuid as another task entry.
- Keep generation logic separate from the guided task flow.
- Default to a standard UUID v4 implementation for the most familiar case.
- Use a small, well-defined alphabet for nanoids so output stays copy-friendly.
- Keep machine-friendly output as the default to preserve composability with pipes and shell scripts.

Practical defaults:

- `uuid` should be the default type.
- `count` should default to `1`.
- `nanoid` should use a length that is useful for common app IDs if the user does not specify one.
- random string mode should use a safe default alphabet that avoids ambiguous characters only if that improves usability without surprising the user.

If clipboard support is implemented, it should be opt-in and should not break normal stdout usage.

## Test Plan

Cover both direct CLI behavior and the guided task flow.

Minimum cases:

- default invocation prints one UUID v4
- `--type nanoid` produces a value of the expected shape
- `--type string --length 24` produces a 24-character value
- `--count 10` prints ten line-separated values
- custom alphabet generation works and respects the supplied alphabet
- invalid counts fail with a useful error
- invalid lengths fail with a useful error
- empty alphabet input fails cleanly
- `--clipboard` works in interactive mode or fails gracefully when unavailable
- `jot task uuid` renders the guided flow and prints the direct-command tip after completion

The tests should validate that the feature remains terminal-first, machine-friendly, and easy to pipe into other commands.
