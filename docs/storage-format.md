# Storage Format

## Goals
- Preserve the existing append-only notebook for human readability and backwards compatibility.
- Add a metadata-friendly format that supports tags, repo metadata, links, and timestamps.
- Enable future sync without rewriting or breaking legacy notes.

## Current format (v1): `journal.txt`
Location: `~/.jot/journal.txt`

Each note is a single line:

```
[YYYY-MM-DD HH:MM] note text...
```

This is the only file written by the current CLI. It remains the canonical, human-first record.

## Proposed format (v2): `journal.ndjson`
Location: `~/.jot/journal.ndjson`

`journal.ndjson` is **optional** and can be generated on demand for sync/metadata-heavy workflows. It is an append-only JSON Lines (NDJSON) file, one JSON object per note.

### Schema
Each entry is a JSON object with the following fields:

| Field | Type | Description |
| --- | --- | --- |
| `id` | string | Unique identifier. Prefer ULID/UUID for sync stability. |
| `text` | string | The note body, as typed. |
| `created_at` | string (RFC3339) | Timestamp when the note was created. |
| `updated_at` | string (RFC3339, optional) | Timestamp when the note was last updated. |
| `tags` | array of strings (optional) | Normalized tags, e.g. `"reading"`, `"project-x"`. |
| `links` | array of objects (optional) | Link metadata (see below). |
| `repo` | object (optional) | Repository metadata (see below). |
| `source` | object (optional) | Provenance for migrations/imports (see below). |

### Links object
Each link entry is:

| Field | Type | Description |
| --- | --- | --- |
| `type` | string | `"url"`, `"file"`, `"issue"`, etc. |
| `target` | string | The link target (URL, path, issue ID). |
| `title` | string (optional) | Human-readable label. |

### Repo object
Use when the note relates to a Git repo or workspace:

| Field | Type | Description |
| --- | --- | --- |
| `root` | string | Absolute path to repo root. |
| `remote` | string (optional) | Primary remote URL. |
| `branch` | string (optional) | Current branch at capture time. |
| `path` | string (optional) | Path within the repo (file or directory). |

### Source object
Used to keep provenance for migrated notes:

| Field | Type | Description |
| --- | --- | --- |
| `journal` | string | Source file name, e.g. `"journal.txt"`. |
| `line` | number | Line number within the source file. |

### Example entry
```
{"id":"018f7f0f4b3e4b20b9e6a6f2d04a5d2c","text":"track this bug from the branch","created_at":"2024-05-09T14:22:00-07:00","tags":["bug"],"links":[{"type":"issue","target":"#128"}],"repo":{"root":"/Users/me/projects/jot","remote":"git@github.com:me/jot.git","branch":"fix/metadata"}}
```

## Compatibility guarantees
- `journal.txt` remains unchanged and readable forever.
- `journal.ndjson` can be generated from `journal.txt` without loss of the original text.
- Tools that only understand `journal.txt` continue to work.

## Migration plan
1. **Generate** `journal.ndjson` from `journal.txt` using the migration tool (see below).
2. **Verify** that line counts match and spot-check timestamps.
3. **Keep** `journal.txt` as the canonical plain-text archive.
4. **Rollback** by deleting `journal.ndjson` (no data loss).

### Migration tool
Use `scripts/migrate-journal-to-ndjson.go` to create `journal.ndjson` from existing notes. The tool:
- Parses timestamps in the `journal.txt` format (`[YYYY-MM-DD HH:MM]`).
- Emits RFC3339 timestamps in `created_at`.
- Preserves the full original text in `text`.
- Adds a `source` block pointing to the source line number.

Example:
```
go run scripts/migrate-journal-to-ndjson.go
```

Optional flags:
- `-in` to specify a custom `journal.txt` path.
- `-out` to specify a custom output `journal.ndjson` path.
- `-force` to overwrite an existing output file.
