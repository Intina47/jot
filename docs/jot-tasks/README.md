# jot Tasks

This folder tracks the next wave of `jot task` specs.

Shared design rules for every task:

- `jot task` is the discovery layer, not the only way to run the feature.
- Every task should have a direct command path that becomes the habit after the first use.
- The CLI stays terminal-first. No browser detours. No upload flows.
- New task specs must preserve the current `jot task` UI direction instead of replacing it with a different interaction model.
- Specs should assume local-first execution and avoid sending user data anywhere.
- Specs should prefer predictable defaults over extra prompts.

Each task spec should cover:

- Goal
- Primary direct-command UX
- `jot task` guided UX
- Inputs
- Outputs
- Flags and options
- Error handling
- Non-goals
- Implementation notes
- Test plan

Current spec files:

- [resize](C:/Users/mamba/jot/docs/jot-tasks/resize.md)
- [minify](C:/Users/mamba/jot/docs/jot-tasks/minify.md)
- [encode](C:/Users/mamba/jot/docs/jot-tasks/encode.md)
- [hash](C:/Users/mamba/jot/docs/jot-tasks/hash.md)
- [diff](C:/Users/mamba/jot/docs/jot-tasks/diff.md)
- [rename](C:/Users/mamba/jot/docs/jot-tasks/rename.md)
- [compress](C:/Users/mamba/jot/docs/jot-tasks/compress.md)
- [qr](C:/Users/mamba/jot/docs/jot-tasks/qr.md)
- [timestamp](C:/Users/mamba/jot/docs/jot-tasks/timestamp.md)
- [uuid](C:/Users/mamba/jot/docs/jot-tasks/uuid.md)
- [strip](C:/Users/mamba/jot/docs/jot-tasks/strip.md)
- [palette](C:/Users/mamba/jot/docs/jot-tasks/palette.md)
