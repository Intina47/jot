# Jot Roadmap: Codex Task Breakdown

This document turns the roadmap into discrete, parallelizable tasks with clear goals, scope, deliverables, and acceptance criteria. Each task is phrased as a prompt suitable for Codex to pick up and execute.

## Stream A — Capture & UX

### Task A1 — Quick Capture CLI Flow
**Goal**: Make capturing a note from the terminal a single command that is fast and predictable.

**Scope**
- Add a `jot capture` (or equivalent) command path that accepts inline content, or launches an editor when content is omitted.
- Support metadata flags for title, tags, and project/repo context.
- Ensure notes are stored in the existing note format and location.

**Deliverables**
- CLI command and help text.
- Unit tests for parsing and storage behavior.
- Documentation snippet in README.

**Acceptance Criteria**
- `jot capture "note" --title "t" --tag foo` writes a note with expected metadata.
- `jot capture` opens the configured editor and saves on exit.
- Tests pass on macOS/Linux/Windows.

---

### Task A2 — Templates for Daily/Meeting Notes
**Goal**: Provide a lightweight templating system so developers can create structured notes quickly.

**Scope**
- Add a template registry with a small built-in set (daily, meeting, RFC).
- Enable `jot new --template daily` and allow custom templates via a config directory.
- Render variables like date/time and repo name.

**Deliverables**
- Template lookup and rendering logic.
- CLI command to list templates.
- Documentation describing how to create custom templates.

**Acceptance Criteria**
- `jot new --template daily` creates a note with today’s date pre-filled.
- Custom template placed in config dir is discovered and used.

---

### Task A3 — Keyboard-First Navigation (Search UX)
**Goal**: Speed up navigation via a quick switcher and fuzzy search.

**Scope**
- Implement a terminal-based quick switcher to search notes by title and tags.
- Provide a minimal TUI flow (list, filter, open).

**Deliverables**
- New CLI command (`jot open` or `jot switch`).
- Integration tests for basic search and open behavior.
- README documentation.

**Acceptance Criteria**
- Searching by partial title returns matches ranked by relevance.
- Selecting a note opens it in the configured editor.

---

## Stream B — Search & Indexing

### Task B1 — Local Full-Text Indexing
**Goal**: Make note search fast, accurate, and incremental.

**Scope**
- Add a local search index with incremental updates (only reindex changed notes).
- Support boolean and phrase queries.

**Deliverables**
- Search index module and CLI `jot search` enhancement.
- Benchmarks for indexing and query latency.
- Tests for incremental updates.

**Acceptance Criteria**
- Searching 1k notes completes in under 200ms on a standard dev laptop.
- Updating a note updates results without full reindex.

---

### Task B2 — Filters (Tags, Date, Project)
**Goal**: Allow developers to slice notes by tag, date, and repo context.

**Scope**
- Extend `jot search` to accept filters for tags, date range, and project/repo.
- Support combinable filters.

**Deliverables**
- Filter parser and integration with search engine.
- Tests for each filter type.
- Updated CLI docs.

**Acceptance Criteria**
- `jot search "incident" --tag prod --since 2024-01-01 --project myrepo` works as expected.

---

## Stream C — Integrations

### Task C1 — Git Repo Awareness
**Goal**: Automatically attach repo/project metadata to notes.

**Scope**
- Detect active git repo in the current working directory.
- Add repo metadata to notes created within the repo.

**Deliverables**
- Repo detection utility.
- Tests for nested repos and worktrees.
- Documentation update.

**Acceptance Criteria**
- Notes created in a repo store repo name/path metadata.
- Notes outside a repo are unaffected.

---

### Task C2 — Link External Issues/PRs
**Goal**: Allow linking issues/PRs to notes via URLs.

**Scope**
- Add `jot link <url>` to associate a URL with a note.
- Store URL metadata and display in note view.

**Deliverables**
- CLI implementation and metadata format.
- Tests for URL parsing and storage.
- Docs example.

**Acceptance Criteria**
- `jot link https://github.com/org/repo/pull/123` attaches the link to the note.

---

## Stream D — Sync Foundations

### Task D1 — Local Watcher + Conflict Spec
**Goal**: Enable safe sync by defining how conflicts are detected and resolved.

**Scope**
- Implement filesystem watcher for note directory.
- Draft a conflict resolution spec (last-write-wins + conflict copies).

**Deliverables**
- Watcher implementation and tests.
- `docs/sync-conflicts.md` describing resolution behavior.

**Acceptance Criteria**
- Concurrent edits create a conflict copy with a deterministic filename.

---

### Task D2 — Storage Format & Migration Plan
**Goal**: Solidify note storage format to support sync and metadata.

**Scope**
- Review current storage and propose updates for metadata extensibility.
- Provide a migration plan/tool if format changes are needed.

**Deliverables**
- `docs/storage-format.md` with schema and rationale.
- Optional migration script (if required).

**Acceptance Criteria**
- Format supports tags, repo metadata, links, and timestamps without breaking existing notes.

---

## Stream E — Collaboration & Sharing (Phase 2)

### Task E1 — Shared Spaces (Design Spec)
**Goal**: Define how shared notebooks/spaces should work for teams.

**Scope**
- Draft functional spec for shared spaces, permissions, and sharing flows.

**Deliverables**
- `docs/shared-spaces.md` spec document.

**Acceptance Criteria**
- Spec covers roles (owner/editor/viewer), link sharing, and auditing.

---

## Stream F — AI Assistance (Phase 3)

### Task F1 — Related Notes Suggestion (Design + Prototype)
**Goal**: Suggest related notes using lightweight similarity.

**Scope**
- Implement a basic TF-IDF or embedding-based similarity for local notes.
- Add CLI command to show related notes for a given note ID.

**Deliverables**
- Prototype implementation and tests.
- README section for usage.

**Acceptance Criteria**
- `jot related <note-id>` lists top 5 related notes.

---

## Stream G — Developer Community & Growth

### Task G1 — Template Library Seed
**Goal**: Provide developer-focused templates to accelerate adoption.

**Scope**
- Create a `templates/` folder with starter templates (meeting, incident, RFC).
- Document how to contribute new templates.

**Deliverables**
- Template files and README section.

**Acceptance Criteria**
- `jot new --template incident` works out of the box.
