// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package kchatfmt converts kChat markdown to Matrix HTML.
package kchatfmt

import (
	"html"
	"regexp"
	"strconv"
	"strings"

	"maunium.net/go/mautrix/event"
)

// ParsedMessage holds the result of converting kChat markdown to Matrix format.
type ParsedMessage struct {
	Body          string
	Format        event.Format
	FormattedBody string
	RelatesTo     *event.RelatesTo
}

var (
	boldRe       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicRe     = regexp.MustCompile(`(?:^|[^*])_(.+?)_(?:[^*]|$)`)
	strikeRe     = regexp.MustCompile(`~~(.+?)~~`)
	codeRe       = regexp.MustCompile("`([^`]+)`")
	codeBlockRe  = regexp.MustCompile("(?s)```(\\w+)?\\n?(.*?)```")
	linkRe       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	headingRe    = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)
	ulRe         = regexp.MustCompile(`(?m)^[-*]\s+(.+)$`)
	olRe         = regexp.MustCompile(`(?m)^\d+\.\s+(.+)$`)
	blockquoteRe = regexp.MustCompile(`(?m)^>\s+(.+)$`)
)

// codeBlock holds extracted code block data.
type codeBlock struct {
	lang    string
	content string
}

// Parse converts a kChat markdown message to Matrix event content.
func Parse(text string) *ParsedMessage {
	if text == "" {
		return &ParsedMessage{}
	}

	hasFormatting := boldRe.MatchString(text) ||
		italicRe.MatchString(text) ||
		strikeRe.MatchString(text) ||
		codeRe.MatchString(text) ||
		codeBlockRe.MatchString(text) ||
		linkRe.MatchString(text) ||
		headingRe.MatchString(text) ||
		blockquoteRe.MatchString(text) ||
		ulRe.MatchString(text) ||
		olRe.MatchString(text)

	if !hasFormatting {
		return &ParsedMessage{Body: text}
	}

	// Step 1: Extract code blocks into placeholders.
	var codeBlocks []codeBlock
	processed := codeBlockRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := codeBlockRe.FindStringSubmatch(match)
		lang := ""
		content := ""
		if len(parts) >= 3 {
			lang = parts[1]
			content = parts[2]
		} else if len(parts) >= 2 {
			content = parts[1]
		}
		idx := len(codeBlocks)
		codeBlocks = append(codeBlocks, codeBlock{lang: lang, content: content})
		return "\x00CODEBLOCK" + strconv.Itoa(idx) + "\x00"
	})

	// Step 2: Process line-by-line for structural elements on raw text.
	lines := strings.Split(processed, "\n")
	var result []string
	var listType string // "ul", "ol", or ""
	var listItems []string

	flushList := func() {
		if len(listItems) == 0 {
			return
		}
		tag := listType
		result = append(result, "<"+tag+">"+strings.Join(listItems, "")+"</"+tag+">")
		listItems = nil
		listType = ""
	}

	for _, line := range lines {
		// Check blockquote.
		if m := blockquoteRe.FindStringSubmatch(line); len(m) >= 2 {
			flushList()
			result = append(result, "<blockquote>"+html.EscapeString(m[1])+"</blockquote>")
			continue
		}

		// Check heading.
		if m := headingRe.FindStringSubmatch(line); len(m) >= 3 {
			flushList()
			level := min(len(m[1]), 6)
			lvl := strconv.Itoa(level)
			result = append(result, "<h"+lvl+">"+html.EscapeString(m[2])+"</h"+lvl+">")
			continue
		}

		// Check unordered list.
		if m := ulRe.FindStringSubmatch(line); len(m) >= 2 {
			if listType != "ul" {
				flushList()
				listType = "ul"
			}
			listItems = append(listItems, "<li>"+html.EscapeString(m[1])+"</li>")
			continue
		}

		// Check ordered list.
		if m := olRe.FindStringSubmatch(line); len(m) >= 2 {
			if listType != "ol" {
				flushList()
				listType = "ol"
			}
			listItems = append(listItems, "<li>"+html.EscapeString(m[1])+"</li>")
			continue
		}

		// Regular line.
		flushList()
		result = append(result, html.EscapeString(line))
	}
	flushList()

	formatted := strings.Join(result, "\n")

	// Step 3: Inline formatting.
	formatted = codeRe.ReplaceAllString(formatted, "<code>$1</code>")
	formatted = boldRe.ReplaceAllString(formatted, "<strong>$1</strong>")
	formatted = italicRe.ReplaceAllString(formatted, "<em>$1</em>")
	formatted = strikeRe.ReplaceAllString(formatted, "<del>$1</del>")

	// Links — only allow safe URL schemes.
	formatted = linkRe.ReplaceAllStringFunc(formatted, func(match string) string {
		parts := linkRe.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		text, href := parts[1], parts[2]
		lower := strings.ToLower(strings.TrimSpace(href))
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:") {
			return `<a href="` + href + `">` + text + `</a>`
		}
		// Unsafe scheme (javascript:, data:, etc.) — render as plain text.
		return text
	})

	// Step 4: Restore code blocks with language hints.
	for i, cb := range codeBlocks {
		placeholder := "\x00CODEBLOCK" + strconv.Itoa(i) + "\x00"
		escapedContent := html.EscapeString(cb.content)
		var replacement string
		if cb.lang != "" {
			replacement = `<pre><code class="language-` + html.EscapeString(cb.lang) + `">` + escapedContent + `</code></pre>`
		} else {
			replacement = `<pre><code>` + escapedContent + `</code></pre>`
		}
		formatted = strings.Replace(formatted, placeholder, replacement, 1)
	}

	// Step 5: Paragraphs (double newlines).
	formatted = strings.ReplaceAll(formatted, "\n\n", "</p><p>")

	// Step 6: Line breaks (remaining single newlines).
	formatted = strings.ReplaceAll(formatted, "\n", "<br/>")

	// Wrap in paragraph tags if we have paragraph breaks.
	if strings.Contains(formatted, "</p><p>") {
		formatted = "<p>" + formatted + "</p>"
	}

	return &ParsedMessage{
		Body:          text,
		Format:        event.FormatHTML,
		FormattedBody: formatted,
	}
}
