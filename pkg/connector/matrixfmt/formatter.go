// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package matrixfmt converts Matrix HTML to kChat markdown.
package matrixfmt

import (
	"regexp"
	"strings"

	"maunium.net/go/mautrix/event"
)

var (
	strongRe     = regexp.MustCompile(`<strong>(.*?)</strong>`)
	emRe         = regexp.MustCompile(`<em>(.*?)</em>`)
	delRe        = regexp.MustCompile(`<del>(.*?)</del>`)
	codeRe       = regexp.MustCompile(`<code>(.*?)</code>`)
	preRe        = regexp.MustCompile(`(?s)<pre><code>(.*?)</code></pre>`)
	linkRe       = regexp.MustCompile(`<a href="([^"]+)"[^>]*>(.*?)</a>`)
	brRe         = regexp.MustCompile(`<br\s*/?>`)
	blockquoteRe = regexp.MustCompile(`(?s)<blockquote>(.*?)</blockquote>`)
	headingRe    = regexp.MustCompile(`<h([1-6])>(.*?)</h[1-6]>`)
	ulRe         = regexp.MustCompile(`(?s)<ul>(.*?)</ul>`)
	olRe         = regexp.MustCompile(`(?s)<ol>(.*?)</ol>`)
	liRe         = regexp.MustCompile(`<li>(.*?)</li>`)
	pRe          = regexp.MustCompile(`(?s)<p>(.*?)</p>`)
	tagRe        = regexp.MustCompile(`<[^>]+>`)
)

// Parse converts Matrix message content to kChat markdown.
func Parse(content *event.MessageEventContent) string {
	if content == nil {
		return ""
	}

	// If no HTML format, return plain text body.
	if content.Format != event.FormatHTML || content.FormattedBody == "" {
		return content.Body
	}

	text := content.FormattedBody

	// Code blocks first (preserve content inside).
	text = preRe.ReplaceAllString(text, "```\n$1\n```")
	text = codeRe.ReplaceAllString(text, "`$1`")

	// Inline formatting.
	text = strongRe.ReplaceAllString(text, "**$1**")
	text = emRe.ReplaceAllString(text, "_${1}_")
	text = delRe.ReplaceAllString(text, "~~$1~~")

	// Links.
	text = linkRe.ReplaceAllString(text, "[$2]($1)")

	// Headings.
	text = headingRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := headingRe.FindStringSubmatch(match)
		level := parts[1][0] - '0'
		prefix := strings.Repeat("#", int(level))
		return prefix + " " + parts[2]
	})

	// Blockquotes.
	text = blockquoteRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := blockquoteRe.FindStringSubmatch(match)
		lines := strings.Split(strings.TrimSpace(parts[1]), "\n")
		for i, line := range lines {
			lines[i] = "> " + strings.TrimSpace(line)
		}
		return strings.Join(lines, "\n")
	})

	// Lists.
	text = ulRe.ReplaceAllStringFunc(text, func(match string) string {
		items := liRe.FindAllStringSubmatch(match, -1)
		var result []string
		for _, item := range items {
			result = append(result, "- "+strings.TrimSpace(item[1]))
		}
		return strings.Join(result, "\n")
	})

	text = olRe.ReplaceAllStringFunc(text, func(match string) string {
		items := liRe.FindAllStringSubmatch(match, -1)
		var result []string
		for i, item := range items {
			result = append(result, strings.Repeat(" ", 0)+string(rune('1'+i))+". "+strings.TrimSpace(item[1]))
		}
		return strings.Join(result, "\n")
	})

	// Paragraphs.
	text = pRe.ReplaceAllString(text, "$1\n\n")

	// Line breaks.
	text = brRe.ReplaceAllString(text, "\n")

	// Strip remaining HTML tags.
	text = tagRe.ReplaceAllString(text, "")

	// Clean up extra whitespace.
	text = strings.TrimSpace(text)

	return text
}
