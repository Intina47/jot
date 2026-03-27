# Sync conflict resolution

## Goals
- Keep local notes safe during sync.
- Prefer the most recent write.
- Preserve conflicting edits in deterministic copies.

## Conflict detection
1. Each note stores its last modified time from the filesystem.
2. During sync, compare the incoming remote modified time to the local modified time.
3. A conflict exists when **both sides changed** since the last sync checkpoint.

## Resolution policy (last-write-wins + conflict copies)
1. If only one side changed, accept that change.
2. If both sides changed:
   - The version with the newest modified time becomes the primary note (last-write-wins).
   - The losing version is written as a conflict copy alongside the primary note.

## Conflict copy naming
Conflict copies are deterministic and derived from the losing version metadata:

```
<original filename>.conflict-<UTC timestamp>-<actor id>
```

Example:

```
journal.txt.conflict-20240506T070809Z-device-01
```

Where:
- `UTC timestamp` is formatted as `YYYYMMDDTHHMMSSZ` from the losing write's modified time.
- `actor id` is the sync client identifier that performed the losing write (defaults to `unknown` when not available).

## File placement
Conflict copies are created in the same directory as the original note so users can discover and resolve them locally.
