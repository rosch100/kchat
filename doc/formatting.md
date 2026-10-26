# Formatting

## Overview

The bridge converts between two markup formats:

- **Matrix**: Uses HTML (specifically `org.matrix.custom.html` format) for rich text
- **kChat**: Uses Markdown for rich text

Two separate formatter packages handle each direction:

| Package | Direction | Input | Output |
|---------|-----------|-------|--------|
| `pkg/connector/matrixfmt` | Matrix to kChat | HTML (`FormattedBody`) | Markdown |
| `pkg/connector/kchatfmt` | kChat to Matrix | Markdown | HTML |

The `formatting.go` file in the connector package provides thin wrappers (`matrixfmtParse` and `kchatfmtParse`) that delegate to the respective packages.

## Matrix HTML to kChat Markdown

**Package**: `pkg/connector/matrixfmt`

**Entry point**: `Parse(content *event.MessageEventContent) string`

If the Matrix message has no HTML format (`Format != FormatHTML` or empty `FormattedBody`), the plain text `Body` is returned as-is.

### Conversion Table

| Matrix HTML | kChat Markdown | Notes |
|-------------|-------------------|-------|
| `<strong>text</strong>` | `**text**` | Bold |
| `<em>text</em>` | `_text_` | Italic |
| `<del>text</del>` | `~~text~~` | Strikethrough |
| `<code>text</code>` | `` `text` `` | Inline code |
| `<pre><code>text</code></pre>` | ` ```\ntext\n``` ` | Code block |
| `<a href="url">text</a>` | `[text](url)` | Links |
| `<h1>text</h1>` ... `<h6>` | `# text` ... `###### text` | Headings |
| `<blockquote>text</blockquote>` | `> text` (per line) | Block quotes |
| `<ul><li>text</li></ul>` | `- text` | Unordered lists |
| `<ol><li>text</li></ol>` | `1. text` | Ordered lists |
| `<p>text</p>` | `text\n\n` | Paragraphs |
| `<br/>` | `\n` | Line breaks |

### Processing Order

1. Code blocks (`<pre><code>`) -- processed first to preserve content
2. Inline code (`<code>`)
3. Bold, italic, strikethrough
4. Links
5. Headings
6. Blockquotes
7. Lists (unordered, then ordered)
8. Paragraphs
9. Line breaks
10. Strip remaining HTML tags
11. Trim whitespace

Code blocks are processed first to prevent inner formatting from being converted. For example, `<pre><code>**not bold**</code></pre>` should produce a code block containing the literal text `**not bold**`, not bold text inside a code block.

## kChat Markdown to Matrix HTML

**Package**: `pkg/connector/kchatfmt`

**Entry point**: `Parse(text string) *ParsedMessage`

If the text contains no Markdown formatting (checked via regex), returns a plain text message with no `Format` or `FormattedBody` set. This avoids sending unnecessary HTML to Matrix.

### Conversion Table

| kChat Markdown | Matrix HTML | Notes |
|-------------------|-------------|-------|
| `**text**` | `<strong>text</strong>` | Bold |
| `_text_` | `<em>text</em>` | Italic |
| `~~text~~` | `<del>text</del>` | Strikethrough |
| `` `text` `` | `<code>text</code>` | Inline code |
| ` ```\ntext\n``` ` | `<pre><code>text</code></pre>` | Code block (with optional language hint) |
| `[text](url)` | `<a href="url">text</a>` | Links |
| `# text` | `<h1>text</h1>` | Headings (h1-h6) |
| `> text` | `<blockquote>text</blockquote>` | Block quotes |
| `- text` | `<ul><li>text</li></ul>` | Unordered lists |
| `1. text` | `<ol><li>text</li></ol>` | Ordered lists |
| `text\n\ntext` | `<p>text</p><p>text</p>` | Paragraphs |
| ` ```lang\ncode\n``` ` | `<pre><code class="language-lang">code</code></pre>` | Code block with language hint |
| `@username` | `@username` | Mentions (passed through, not yet linked) |
| `\n` | `<br/>` | Line breaks |

### Processing Order

1. Format detection (skip HTML generation for plain text)
2. Code block extraction into placeholders (protects content from further processing)
3. Line-by-line structural processing on raw text: blockquotes, headings, lists
4. HTML-escape remaining inline text
5. Inline formatting: inline code, bold, italic, strikethrough, links
6. Restore code blocks with language hints
7. Paragraph wrapping (double newlines)
8. Line breaks (remaining single newlines)

Structural elements (blockquotes, headings, lists) are processed before HTML escaping to avoid the `>` character being escaped to `&gt;` before blockquote detection. Code blocks are extracted first to protect their content from all formatting passes.

### Return Value

```go
type ParsedMessage struct {
    Body          string          // Plain text body (always set)
    Format        event.Format    // "org.matrix.custom.html" or empty
    FormattedBody string          // HTML body or empty
    RelatesTo     *event.RelatesTo // For replies/edits (reserved)
}
```

## Supported Elements Summary

| Element | Matrix to MM | MM to Matrix |
|---------|:---:|:---:|
| Bold | Yes | Yes |
| Italic | Yes | Yes |
| Strikethrough | Yes | Yes |
| Inline code | Yes | Yes |
| Code block | Yes | Yes |
| Links | Yes | Yes |
| Headings (1-6) | Yes | Yes |
| Block quotes | Yes | Yes |
| Unordered lists | Yes | Yes |
| Ordered lists | Yes | Yes |
| Paragraphs | Yes | Yes |
| Line breaks | Yes | Yes |
| Mentions | No | Passthrough |
| Images/embeds | No | No |

## Known Limitations

- **Nested formatting**: The regex-based approach does not handle deeply nested formatting (e.g., bold inside italic inside a list item). Each pattern is applied independently.
- **Italic edge cases**: The italic regex in kChat-to-Matrix requires non-asterisk characters around underscores to avoid false matches with URLs containing underscores.
- **Tables**: Neither direction supports table conversion.
- **Mentions**: kChat `@username` mentions are not specifically handled and pass through as plain text. They are not converted to Matrix user mention pills.

## Adding Support for New Elements

To add a new formatting element:

1. **Matrix to kChat** (`matrixfmt/formatter.go`):
   - Add a regex matching the HTML pattern (add to the `var` block)
   - Add a `ReplaceAllString` or `ReplaceAllStringFunc` call in `Parse()` at the appropriate position in the processing order
   - Ensure it runs before the final HTML tag stripping

2. **kChat to Matrix** (`kchatfmt/formatter.go`):
   - Add a regex matching the Markdown pattern (add to the `var` block)
   - Add the regex to the `hasFormatting` detection check
   - Add a `ReplaceAllString` or `ReplaceAllStringFunc` call in `Parse()` at the appropriate position
   - Ensure it runs after HTML escaping

3. **Tests**: Both packages have corresponding `_test.go` files. Add test cases for the new element in both directions.
