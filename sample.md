# Markdown Feature Test — sample.md

A comprehensive document for testing every markdown feature that jot's renderer handles.
Use this file with `jot open sample.md` or `jot write sample.md` to verify rendering.

---

## Table of Contents

- [Headings](#headings)
- [Paragraphs and Line Breaks](#paragraphs-and-line-breaks)
- [Inline Formatting](#inline-formatting)
- [Blockquotes](#blockquotes)
- [Lists](#lists)
- [Task Checklists](#task-checklists)
- [Code](#code)
- [Tables](#tables)
- [Horizontal Rules](#horizontal-rules)
- [Links](#links)
- [Mixed and Nested](#mixed-and-nested)

---

## Headings

# H1 — The quick brown fox
## H2 — jumps over the lazy dog
### H3 — Pack my box with five dozen liquor jugs
#### H4 — How vexingly quick daft zebras jump
##### H5 — The five boxing wizards jump quickly
###### H6 — Sphinx of black quartz, judge my vow

---

## Paragraphs and Line Breaks

This is a normal paragraph. The renderer should join these two
lines with a single space, not a hard break. Lorem ipsum dolor sit amet,
consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore.

This is a second paragraph, separated from the first by a blank line.
Paragraphs should have visible spacing between them.

These lines use a hard break (two trailing spaces):  
First line ends with two spaces.  
Second line ends with two spaces.  
Third line. Each should appear on its own line in the output.

These lines use a hard break (trailing backslash):\
Line one.\
Line two.\
Line three. Same visual result as the two-space variant.

This paragraph intentionally tests poetry-style content where every line break matters:  
Shall I compare thee to a summer's day?  
Thou art more lovely and more temperate.  
Rough winds do shake the darling buds of May,  
And summer's lease hath all too short a date.

---

## Inline Formatting

Plain text with **bold using double asterisks** and more plain text.

Plain text with __bold using double underscores__ and more plain text.

Plain text with *italic using single asterisk* and more plain text.

Plain text with _italic using single underscore_ and more plain text.

Plain text with ***bold and italic combined*** and more plain text.

Plain text with ~~strikethrough~~ and more plain text.

Inline `code span` inside a sentence.

Multiple inline styles: **bold**, _italic_, `code`, and ~~strikethrough~~ all in one line.

Nested inline: **this is bold and _this part is also italic_ back to just bold**.

Escaped characters: \*not italic\*, \`not code\`, \~\~not strikethrough\~\~.

---

## Blockquotes

> This is a single-level blockquote. It should have a left border and slightly
> different background from the main content area.

> A blockquote can also contain **bold**, _italic_, and `code` inline styles.
> It wraps normally across multiple lines within the quote.

> ### A heading inside a blockquote
>
> Blockquotes can contain other block elements like headings and paragraphs.
> This paragraph is inside the same blockquote as the heading above.

> First level of nesting.
>
> > Second level of nesting. The double `>>` should render as a blockquote
> > inside a blockquote, visually indented further.
> >
> > > Third level of nesting. Three levels deep. This tests recursive
> > > blockquote rendering all the way down.
> >
> > Back to second level.
>
> Back to first level.

---

## Lists

### Unordered lists

- Alpha
- Beta
- Gamma
- Delta

Unordered list using asterisks:

* One
* Two
* Three

Unordered list using plus signs:

+ Red
+ Green
+ Blue

### Ordered lists

1. First item
2. Second item
3. Third item
4. Fourth item
5. Fifth item

Ordered list with loose numbering (markdown ignores the actual numbers):

1. Item one
1. Item two
1. Item three

### Nested lists

- Top level item A
  - Nested item A1
  - Nested item A2
    - Doubly nested A2a
    - Doubly nested A2b
  - Nested item A3
- Top level item B
  - Nested item B1
- Top level item C

Ordered with nested unordered:

1. Install dependencies
   - Run `npm install`
   - Run `pip install -r requirements.txt`
2. Configure environment
   - Copy `.env.example` to `.env`
   - Fill in the required values
3. Start the server
   - Development: `npm run dev`
   - Production: `npm start`

Unordered with nested ordered:

- Frontend tasks
  1. Write component tests
  2. Fix accessibility issues
  3. Update Storybook stories
- Backend tasks
  1. Add rate limiting
  2. Write migration scripts
  3. Update API documentation

### Lists with continuation paragraphs

- This list item has a continuation paragraph.

  The continuation is indented to align with the list content. It should
  render as part of the same `<li>`, not as a new paragraph outside the list.

- This is the next item, back to normal.

- Another item with continuation:

  First continuation paragraph.

  Second continuation paragraph in the same item.

- Final item.

---

## Task Checklists

### Project setup

- [x] Create repository
- [x] Set up CI pipeline
- [x] Write contributing guide
- [ ] Add code coverage reporting
- [ ] Set up staging environment

### Release checklist for v2.0

- [x] Feature freeze
- [x] Update changelog
- [x] Bump version numbers
- [ ] Tag release commit
- [ ] Publish to package registry
- [ ] Post announcement

### Mixed nested tasks

- [x] Backend
  - [x] Auth endpoints
  - [x] User CRUD
  - [ ] Rate limiting
  - [ ] Admin panel
- [ ] Frontend
  - [x] Login page
  - [ ] Dashboard
  - [ ] Settings page
- [ ] Documentation
  - [ ] API reference
  - [ ] Deployment guide

---

## Code

### Inline code

Use `git status` to see what has changed. Run `go build ./...` to compile.
The variable `MAX_RETRIES` is defined in `config.go`.

### Fenced code blocks

Plain fenced block (no language):

```
This is a plain code block.
No syntax highlighting hint is given.
    Indentation is preserved exactly.
```

Go:

```go
package main

import (
	"fmt"
	"strings"
)

// renderMarkdownHTML converts a markdown string to HTML.
func renderMarkdownHTML(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	return renderMarkdownLines(lines)
}

func main() {
	md := "# Hello\n\nThis is **bold** and _italic_."
	html := renderMarkdownHTML(md)
	fmt.Println(html)
}
```

Shell:

```shell
# Clone and build
git clone https://github.com/example/jot.git
cd jot
go build -o jot .

# Run the viewer
./jot open README.md
```

JSON:

```json
{
  "id": "dg0ftbuoqqdc-62",
  "created_at": "2026-03-22T09:15:00Z",
  "title": "Ship the help refresh",
  "tags": ["cli", "release"],
  "project": "jot",
  "content": "Cut the release once tests pass."
}
```

SQL:

```sql
SELECT
  e.id,
  e.title,
  e.created_at,
  GROUP_CONCAT(t.name, ', ') AS tags
FROM entries e
LEFT JOIN entry_tags et ON et.entry_id = e.id
LEFT JOIN tags t ON t.id = et.tag_id
WHERE e.project = 'jot'
GROUP BY e.id
ORDER BY e.created_at DESC
LIMIT 20;
```

Python:

```python
def parse_table_row(line: str) -> list[str]:
    """Split a GFM table row, ignoring pipes inside backtick spans."""
    cells = []
    cell = []
    in_code = False
    i = 0
    while i < len(line):
        ch = line[i]
        if ch == '`':
            in_code = not in_code
            cell.append(ch)
        elif ch == '\\' and not in_code and i + 1 < len(line) and line[i + 1] == '|':
            cell.append('|')
            i += 1
        elif ch == '|' and not in_code:
            cells.append(''.join(cell).strip())
            cell = []
        else:
            cell.append(ch)
        i += 1
    if cell or cells:
        cells.append(''.join(cell).strip())
    return cells
```

---

## Tables

### Basic table

| Name    | Role      | Location   |
|---------|-----------|------------|
| Alice   | Engineer  | Edinburgh  |
| Bob     | Designer  | London     |
| Charlie | PM        | Berlin     |
| Diana   | QA        | Amsterdam  |

### Table with alignment

| Left aligned | Center aligned | Right aligned |
|:-------------|:--------------:|--------------:|
| Apple        | Banana         | Cherry        |
| 1.00         | 2.50           | 100.00        |
| short        | medium text    | a longer cell |

### Table with inline formatting in cells

| Feature         | Status      | Notes                          |
|-----------------|-------------|--------------------------------|
| **Tables**      | ✅ Fixed    | Pipe-aware cell splitting      |
| **Hard breaks** | ✅ Fixed    | Trailing `  ` and `\`         |
| ~~Old parser~~  | ❌ Removed  | Split naively on every `\|`    |
| _Nested lists_  | ✅ Works    | Recursive block renderer       |
| Task checkboxes | ✅ Works    | `- [x]` and `- [ ]`           |

### Table with code in cells (the previously broken case)

| Expression       | Meaning                        | Example              |
|------------------|--------------------------------|----------------------|
| `a\|b`           | Bitwise OR of a and b          | `0b1010\|0b0101`     |
| `a&b`            | Bitwise AND                    | `0xFF & mask`        |
| `a^b`            | Bitwise XOR                    | `flags ^ CLEAR_BIT`  |
| `~a`             | Bitwise NOT                    | `~0x00`              |

### Table with right-oriented rendering (from the screenshot)

| Right oriented rendering |
|--------------------------:|
| 150.0                     |
| or text                   |

### Table with centered rendering

| Centered rendering |
|:------------------:|
| 150.0              |
| or text            |

### Wider reference table

| Command                  | Description                              | Example                          |
|--------------------------|------------------------------------------|----------------------------------|
| `jot`                    | Open the quick prompt                    | `jot`                            |
| `jot init`               | Same as bare `jot`                       | `jot init`                       |
| `jot new`                | Create a note from a template            | `jot new --template meeting`     |
| `jot open`               | Open a file or entry by id               | `jot open dg0ftbuoqqdc-62`       |
| `jot list`               | Browse journal and note entries          | `jot list --full`                |
| `jot capture`            | Capture a structured note                | `jot capture "idea" --tag cli`   |
| `jot write`              | Edit a markdown file in the terminal     | `jot write README.md`            |
| `jot templates`          | List available templates                 | `jot templates`                  |
| `jot integrate windows`  | Add Explorer context menu entry          | `jot integrate windows`          |

---

## Horizontal Rules

Three hyphens:

---

Three asterisks:

***

Three underscores:

___

---

## Links

A bare URL referenced inline: [Anthropic](https://www.anthropic.com).

A link with a longer label: [Go standard library documentation](https://pkg.go.dev/std).

A link inside a sentence: visit [the jot repository](https://github.com/example/jot) for source.

A link inside **bold text**: **[important link](https://example.com)**.

A link inside a list item:

- [GitHub](https://github.com) — code hosting
- [pkg.go.dev](https://pkg.go.dev) — Go package docs
- [CommonMark spec](https://spec.commonmark.org) — markdown reference

A link inside a table cell:

| Resource             | URL                                    |
|----------------------|----------------------------------------|
| Go documentation     | [pkg.go.dev](https://pkg.go.dev)       |
| CommonMark spec      | [spec.commonmark.org](https://spec.commonmark.org) |

---

## Mixed and Nested

### Blockquote containing a list

> Here is a blockquote that contains a bullet list:
>
> - First point inside the quote
> - Second point inside the quote
> - Third point inside the quote
>
> And a follow-up sentence after the list, still inside the blockquote.

### Blockquote containing a code block

> The fix involves changing one line:
>
> ```go
> if len(core) < 1 || strings.Trim(core, "-") != "" {
> ```
>
> Previously the check used `< 3`, which rejected valid short separators.

### List containing a blockquote

- Normal list item before the quote.
- A list item that references a quote:

  > This is a blockquote nested inside a list item continuation.
  > It should render with the blockquote styling indented under the bullet.

- Normal list item after the quote.

### List containing a code block

- Install Go:

  ```shell
  sudo apt install golang-go
  ```

- Verify the installation:

  ```shell
  go version
  ```

- Build the project:

  ```shell
  go build -o jot ./...
  ```

### A complex realistic section (RFC-style)

#### Background

The original `parseMarkdownTableRow` function used `strings.Split(line, "|")` which
does not account for `|` characters appearing inside inline code spans within a cell.
The GFM spec explicitly states that parsers must handle this case.

#### Problem statement

Given a table row like:

```
| `a|b` | normal cell |
```

The naive split produces **three** cells — `` `a ``, `b` ``, and `normal cell` —
instead of the correct **two**: `` `a|b` `` and `normal cell`. This causes:

1. Column count mismatch between header and body rows.
2. Alignment hints applied to the wrong columns.
3. The entire table falling back to raw text in some renderers.

#### Proposed solution

Replace the split with a character-by-character scanner that tracks backtick state:

| Step | Action                                      | State change       |
|-----:|---------------------------------------------|--------------------|
| 1    | Encounter `` ` ``                           | Toggle `inCode`    |
| 2    | Encounter `\|` (escaped pipe)               | Write `\|` to cell |
| 3    | Encounter `\|` while `inCode = true`        | Write `\|` to cell |
| 4    | Encounter `\|` while `inCode = false`       | Flush cell, start next |

#### Acceptance criteria

- [ ] Table with `` `a|b` `` in a cell renders correctly
- [ ] Table with `\|` escaped pipe renders a literal pipe
- [ ] Alignment row `| :---: | ---: |` is parsed correctly
- [ ] Single-column tables with alignment still render
- [x] All existing table tests continue to pass

---

*End of sample.md — if everything above renders correctly, the markdown engine is working.*
