// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package kchatfmt

import (
	"strings"
	"testing"

	"maunium.net/go/mautrix/event"
)

func TestParseEmpty(t *testing.T) {
	t.Parallel()
	result := Parse("")
	if result.Body != "" {
		t.Errorf("empty input Body: got %q", result.Body)
	}
	if result.FormattedBody != "" {
		t.Errorf("empty input FormattedBody: got %q", result.FormattedBody)
	}
}

func TestParsePlainText(t *testing.T) {
	t.Parallel()
	result := Parse("hello world")
	if result.Body != "hello world" {
		t.Errorf("Body: got %q, want %q", result.Body, "hello world")
	}
	if result.Format != "" {
		t.Errorf("plain text should have no format, got %q", result.Format)
	}
	if result.FormattedBody != "" {
		t.Errorf("plain text should have no FormattedBody, got %q", result.FormattedBody)
	}
}

func TestParseBold(t *testing.T) {
	t.Parallel()
	result := Parse("**bold text**")
	if result.Format != event.FormatHTML {
		t.Errorf("Format: got %q, want %q", result.Format, event.FormatHTML)
	}
	if result.Body != "**bold text**" {
		t.Errorf("Body should preserve original: got %q", result.Body)
	}
	if !strings.Contains(result.FormattedBody, "<strong>bold text</strong>") {
		t.Errorf("FormattedBody: got %q, want to contain <strong>bold text</strong>", result.FormattedBody)
	}
}

func TestParseStrikethrough(t *testing.T) {
	t.Parallel()
	result := Parse("~~deleted~~")
	if result.Format != event.FormatHTML {
		t.Errorf("Format: got %q, want %q", result.Format, event.FormatHTML)
	}
	if !strings.Contains(result.FormattedBody, "<del>deleted</del>") {
		t.Errorf("FormattedBody: got %q, want to contain <del>deleted</del>", result.FormattedBody)
	}
}

func TestParseInlineCode(t *testing.T) {
	t.Parallel()
	result := Parse("use `fmt.Println`")
	if result.Format != event.FormatHTML {
		t.Errorf("Format: got %q", result.Format)
	}
	if !strings.Contains(result.FormattedBody, "<code>fmt.Println</code>") {
		t.Errorf("FormattedBody: got %q, want to contain <code>", result.FormattedBody)
	}
}

func TestParseCodeBlock(t *testing.T) {
	t.Parallel()
	input := "```go\nfmt.Println(\"hi\")\n```"
	result := Parse(input)
	if result.Format != event.FormatHTML {
		t.Errorf("Format: got %q", result.Format)
	}
	if result.Body != input {
		t.Errorf("Body should preserve original: got %q", result.Body)
	}
	if !strings.Contains(result.FormattedBody, "<pre><code") {
		t.Errorf("FormattedBody should contain <pre><code, got %q", result.FormattedBody)
	}
}

func TestParseLink(t *testing.T) {
	t.Parallel()
	result := Parse("[example](https://example.com)")
	if result.Format != event.FormatHTML {
		t.Errorf("Format: got %q", result.Format)
	}
	expected := `<a href="https://example.com">example</a>`
	if !strings.Contains(result.FormattedBody, expected) {
		t.Errorf("FormattedBody: got %q, want to contain %q", result.FormattedBody, expected)
	}
}

func TestParseLinkJavascriptFiltered(t *testing.T) {
	t.Parallel()
	result := Parse("[click](javascript:alert(1))")
	if strings.Contains(result.FormattedBody, "javascript:") {
		t.Errorf("javascript: URL should be filtered, got %q", result.FormattedBody)
	}
	if !strings.Contains(result.Body, "click") || !strings.Contains(result.Body, "javascript") {
		t.Errorf("Body should preserve original text, got %q", result.Body)
	}
}

func TestParseLinkDataURIFiltered(t *testing.T) {
	t.Parallel()
	result := Parse("[img](data:text/html,<script>alert(1)</script>)")
	if strings.Contains(result.FormattedBody, "data:") {
		t.Errorf("data: URL should be filtered, got %q", result.FormattedBody)
	}
}

func TestParseHeading(t *testing.T) {
	t.Parallel()
	result := Parse("## Section Title")
	if result.Format != event.FormatHTML {
		t.Errorf("Format: got %q", result.Format)
	}
	if !strings.Contains(result.FormattedBody, "<h2>") {
		t.Errorf("FormattedBody should contain <h2>, got %q", result.FormattedBody)
	}
	if !strings.Contains(result.FormattedBody, "Section Title") {
		t.Errorf("FormattedBody should contain heading text, got %q", result.FormattedBody)
	}
}

func TestParseBlockquote(t *testing.T) {
	t.Parallel()
	result := Parse("> quoted text")
	if result.Format != event.FormatHTML {
		t.Errorf("Format: got %q", result.Format)
	}
	if !strings.Contains(result.FormattedBody, "<blockquote>quoted text</blockquote>") {
		t.Errorf("FormattedBody should contain <blockquote>quoted text</blockquote>, got %q", result.FormattedBody)
	}
}

func TestParseRelatesToNil(t *testing.T) {
	t.Parallel()
	result := Parse("**test**")
	if result.RelatesTo != nil {
		t.Error("RelatesTo should be nil for regular messages")
	}
}

func TestParseUnorderedList(t *testing.T) {
	t.Parallel()
	result := Parse("- item one\n- item two")
	if result.Format != event.FormatHTML {
		t.Errorf("Format: got %q", result.Format)
	}
	if !strings.Contains(result.FormattedBody, "<ul>") {
		t.Errorf("should contain <ul>, got %q", result.FormattedBody)
	}
	if !strings.Contains(result.FormattedBody, "<li>item one</li>") {
		t.Errorf("should contain <li>item one</li>, got %q", result.FormattedBody)
	}
	if !strings.Contains(result.FormattedBody, "<li>item two</li>") {
		t.Errorf("should contain <li>item two</li>, got %q", result.FormattedBody)
	}
}

func TestParseOrderedList(t *testing.T) {
	t.Parallel()
	result := Parse("1. first\n2. second")
	if result.Format != event.FormatHTML {
		t.Errorf("Format: got %q", result.Format)
	}
	if !strings.Contains(result.FormattedBody, "<ol>") {
		t.Errorf("should contain <ol>, got %q", result.FormattedBody)
	}
	if !strings.Contains(result.FormattedBody, "<li>first</li>") {
		t.Errorf("should contain <li>first</li>, got %q", result.FormattedBody)
	}
}

func TestParseCodeBlockWithLanguage(t *testing.T) {
	t.Parallel()
	result := Parse("```go\nfmt.Println(\"hi\")\n```")
	if result.Format != event.FormatHTML {
		t.Errorf("Format: got %q", result.Format)
	}
	if !strings.Contains(result.FormattedBody, `class="language-go"`) {
		t.Errorf("should contain language class, got %q", result.FormattedBody)
	}
}

func TestParseCodeBlockNoLanguage(t *testing.T) {
	t.Parallel()
	result := Parse("```\nsome code\n```")
	if result.Format != event.FormatHTML {
		t.Errorf("Format: got %q", result.Format)
	}
	if strings.Contains(result.FormattedBody, "class=") {
		t.Errorf("should not contain language class when none specified, got %q", result.FormattedBody)
	}
	if !strings.Contains(result.FormattedBody, "<pre><code>") {
		t.Errorf("should contain <pre><code>, got %q", result.FormattedBody)
	}
}

func TestParseParagraphs(t *testing.T) {
	t.Parallel()
	result := Parse("**para one**\n\npara two")
	if result.Format != event.FormatHTML {
		t.Errorf("Format: got %q", result.Format)
	}
	if !strings.Contains(result.FormattedBody, "</p><p>") {
		t.Errorf("should contain paragraph break, got %q", result.FormattedBody)
	}
}

func TestParseBlockquoteWithFormatting(t *testing.T) {
	t.Parallel()
	result := Parse("> **bold quote**")
	if !strings.Contains(result.FormattedBody, "<blockquote>") {
		t.Errorf("should contain <blockquote>, got %q", result.FormattedBody)
	}
}

func TestParseCodeBlockProtectsContent(t *testing.T) {
	t.Parallel()
	result := Parse("```\n**not bold** > not quote\n```")
	if strings.Contains(result.FormattedBody, "<strong>") {
		t.Errorf("code block content should not be formatted as bold, got %q", result.FormattedBody)
	}
	if strings.Contains(result.FormattedBody, "<blockquote>") {
		t.Errorf("code block content should not be formatted as blockquote, got %q", result.FormattedBody)
	}
}

// TestParse_NullBytes verifies Parse handles null bytes in input gracefully.
func TestParse_NullBytes(t *testing.T) {
	t.Parallel()
	result := Parse("hello\x00world")
	if result.Body != "hello\x00world" {
		t.Errorf("Body should preserve null bytes, got %q", result.Body)
	}
}

// TestParse_HTMLInjection verifies that user-supplied HTML is escaped in output
// and not rendered as raw tags.
func TestParse_HTMLInjection(t *testing.T) {
	t.Parallel()
	result := Parse("<script>alert(1)</script>")
	if strings.Contains(result.FormattedBody, "<script>") {
		t.Errorf("HTML should be escaped in output, got %q", result.FormattedBody)
	}
	// The Body should always preserve the original text.
	if result.Body != "<script>alert(1)</script>" {
		t.Errorf("Body should preserve original, got %q", result.Body)
	}
}

// TestParse_NestedFormatting verifies handling of nested markdown formatting.
func TestParse_NestedFormatting(t *testing.T) {
	t.Parallel()
	result := Parse("**bold and ~~strike~~**")
	if result.Format != event.FormatHTML {
		t.Errorf("Format: got %q", result.Format)
	}
	if !strings.Contains(result.FormattedBody, "<strong>") {
		t.Errorf("should contain <strong>, got %q", result.FormattedBody)
	}
}

// TestParse_OnlyWhitespace verifies Parse handles whitespace-only input.
func TestParse_OnlyWhitespace(t *testing.T) {
	t.Parallel()
	result := Parse("   \n\n   ")
	if result.Body != "   \n\n   " {
		t.Errorf("Body should preserve whitespace, got %q", result.Body)
	}
}

// TestParse_BodyAlwaysPreservesOriginal verifies the Body field always
// contains the original unmodified input text.
func TestParse_BodyAlwaysPreservesOriginal(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"**bold**",
		"~~strike~~",
		"`code`",
		"[link](https://example.com)",
		"> quote",
		"# heading",
		"- list item",
		"1. ordered item",
		"```\ncode block\n```",
	}

	for _, input := range inputs {
		result := Parse(input)
		if result.Body != input {
			t.Errorf("Body should preserve %q, got %q", input, result.Body)
		}
	}
}

// FuzzParse verifies that the markdown parser never panics for arbitrary input.
// This is a required fuzz test for a parsing function.
func FuzzParse(f *testing.F) {
	// Seed corpus with interesting edge cases.
	f.Add("hello world")
	f.Add("")
	f.Add("**bold**")
	f.Add("~~strike~~")
	f.Add("`code`")
	f.Add("```go\ncode\n```")
	f.Add("[link](https://example.com)")
	f.Add("[xss](javascript:alert(1))")
	f.Add("[data](data:text/html,<script>)")
	f.Add("> blockquote")
	f.Add("# heading")
	f.Add("- list\n- items")
	f.Add("1. ordered\n2. list")
	f.Add("**nested ~~strike~~**")
	f.Add("<script>alert(1)</script>")
	f.Add(string(make([]byte, 500)))
	f.Add("hello\x00world\x01\x02")
	f.Add(strings.Repeat("**bold**", 100))
	f.Add("```\n" + strings.Repeat("x", 1000) + "\n```")

	f.Fuzz(func(t *testing.T, input string) {
		// Should never panic for any input.
		result := Parse(input)

		// Body must always equal the original input.
		if result.Body != input && input != "" {
			t.Errorf("Body should equal input for non-empty strings")
		}

		// FormattedBody should not contain raw <script> tags.
		if strings.Contains(result.FormattedBody, "<script>") {
			t.Error("FormattedBody should never contain raw <script> tags")
		}

		// FormattedBody should not contain javascript: URLs.
		if strings.Contains(result.FormattedBody, "javascript:") {
			t.Error("FormattedBody should never contain javascript: URLs")
		}

		// FormattedBody should not contain data: URLs.
		if strings.Contains(result.FormattedBody, "data:") {
			t.Error("FormattedBody should never contain data: URLs")
		}
	})
}
