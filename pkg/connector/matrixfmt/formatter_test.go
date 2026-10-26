// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package matrixfmt

import (
	"strings"
	"testing"

	"maunium.net/go/mautrix/event"
)

func TestParseNilContent(t *testing.T) {
	t.Parallel()
	result := Parse(nil)
	if result != "" {
		t.Errorf("nil content: got %q, want empty", result)
	}
}

func TestParsePlainText(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body: "hello world",
	}
	result := Parse(content)
	if result != "hello world" {
		t.Errorf("plain text: got %q, want %q", result, "hello world")
	}
}

func TestParseNoFormat(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body:          "plain",
		FormattedBody: "<b>ignored</b>",
	}
	result := Parse(content)
	if result != "plain" {
		t.Errorf("no format: got %q, want %q", result, "plain")
	}
}

func TestParseEmptyFormattedBody(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body:          "fallback",
		Format:        event.FormatHTML,
		FormattedBody: "",
	}
	result := Parse(content)
	if result != "fallback" {
		t.Errorf("empty formatted body: got %q, want %q", result, "fallback")
	}
}

func TestParseBold(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body:          "bold text",
		Format:        event.FormatHTML,
		FormattedBody: "<strong>bold text</strong>",
	}
	result := Parse(content)
	if result != "**bold text**" {
		t.Errorf("bold: got %q, want %q", result, "**bold text**")
	}
}

func TestParseItalic(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body:          "italic text",
		Format:        event.FormatHTML,
		FormattedBody: "<em>italic text</em>",
	}
	result := Parse(content)
	if !strings.Contains(result, "_") {
		t.Errorf("italic should contain underscore markers, got %q", result)
	}
	if !strings.Contains(result, "italic text") {
		t.Errorf("italic should preserve text content, got %q", result)
	}
}

func TestParseStrikethrough(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body:          "deleted",
		Format:        event.FormatHTML,
		FormattedBody: "<del>deleted</del>",
	}
	result := Parse(content)
	if result != "~~deleted~~" {
		t.Errorf("strikethrough: got %q, want %q", result, "~~deleted~~")
	}
}

func TestParseInlineCode(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body:          "code",
		Format:        event.FormatHTML,
		FormattedBody: "<code>fmt.Println</code>",
	}
	result := Parse(content)
	if result != "`fmt.Println`" {
		t.Errorf("inline code: got %q, want %q", result, "`fmt.Println`")
	}
}

func TestParseCodeBlock(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body:          "code",
		Format:        event.FormatHTML,
		FormattedBody: "<pre><code>func main() {}</code></pre>",
	}
	result := Parse(content)
	if !strings.Contains(result, "```") {
		t.Errorf("code block should contain triple backticks, got %q", result)
	}
	if !strings.Contains(result, "func main() {}") {
		t.Errorf("code block should contain code, got %q", result)
	}
}

func TestParseLink(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body:          "example",
		Format:        event.FormatHTML,
		FormattedBody: `<a href="https://example.com">example</a>`,
	}
	result := Parse(content)
	if result != "[example](https://example.com)" {
		t.Errorf("link: got %q, want %q", result, "[example](https://example.com)")
	}
}

func TestParseHeading(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body:          "Title",
		Format:        event.FormatHTML,
		FormattedBody: "<h2>Title</h2>",
	}
	result := Parse(content)
	if result != "## Title" {
		t.Errorf("heading: got %q, want %q", result, "## Title")
	}
}

func TestParseBlockquote(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body:          "quoted",
		Format:        event.FormatHTML,
		FormattedBody: "<blockquote>quoted text</blockquote>",
	}
	result := Parse(content)
	if !strings.Contains(result, "> quoted text") {
		t.Errorf("blockquote: got %q, want to contain %q", result, "> quoted text")
	}
}

func TestParseUnorderedList(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body:          "list",
		Format:        event.FormatHTML,
		FormattedBody: "<ul><li>first</li><li>second</li></ul>",
	}
	result := Parse(content)
	if !strings.Contains(result, "- first") {
		t.Errorf("ul: got %q, want to contain %q", result, "- first")
	}
	if !strings.Contains(result, "- second") {
		t.Errorf("ul: got %q, want to contain %q", result, "- second")
	}
}

func TestParseLineBreak(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body:          "line1\nline2",
		Format:        event.FormatHTML,
		FormattedBody: "line1<br/>line2",
	}
	result := Parse(content)
	if !strings.Contains(result, "line1\nline2") {
		t.Errorf("br: got %q, want to contain newline", result)
	}
}

func TestParseStripsTags(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		Body:          "text",
		Format:        event.FormatHTML,
		FormattedBody: "<div><span>clean text</span></div>",
	}
	result := Parse(content)
	if strings.Contains(result, "<") {
		t.Errorf("should strip HTML tags, got %q", result)
	}
	if !strings.Contains(result, "clean text") {
		t.Errorf("should preserve text content, got %q", result)
	}
}
