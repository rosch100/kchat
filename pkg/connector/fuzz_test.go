// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"maunium.net/go/mautrix/event"
)

// ---------------------------------------------------------------------------
// FuzzIsBridgeUsername — tests the username pattern matching with arbitrary
// strings. No input should cause a panic. Verifies determinism.
// ---------------------------------------------------------------------------

func FuzzIsBridgeUsername(f *testing.F) {
	f.Add("kchat_ghost", "")
	f.Add("kchat-bridge", "")
	f.Add("normaluser", "")
	f.Add("", "")
	f.Add("kchat_", "bridge_")
	f.Add("bridge_bot", "bridge_")
	f.Add("kchat", "")
	f.Add(string([]byte{0x00}), "") // null byte

	f.Fuzz(func(t *testing.T, username, botPrefix string) {
		result := isBridgeUsername(username, botPrefix)

		// Determinism: calling twice with the same input yields the same result.
		result2 := isBridgeUsername(username, botPrefix)
		if result != result2 {
			t.Errorf("non-deterministic: isBridgeUsername(%q, %q) returned %v then %v",
				username, botPrefix, result, result2)
		}

		// Invariant: hardcoded bridge names always match regardless of prefix.
		if username == "kchat-bridge" && !result {
			t.Errorf("kchat-bridge should always match, got false with prefix %q", botPrefix)
		}
	})
}

// ---------------------------------------------------------------------------
// FuzzReactionToEmoji — tests the kChat emoji name → display string
// conversion. No input should cause a panic.
// ---------------------------------------------------------------------------

func FuzzReactionToEmoji(f *testing.F) {
	f.Add("+1")
	f.Add("heart")
	f.Add("custom_emoji")
	f.Add("")
	f.Add(string([]byte{0x00}))
	f.Add("fire")
	f.Add("a very long emoji name that probably does not exist in the map")

	f.Fuzz(func(t *testing.T, name string) {
		result := reactionToEmoji(name)

		// Should never return empty for non-empty input (custom emojis get ":name:").
		// Empty input returns "::" which is non-empty.
		if name != "" && result == "" {
			t.Errorf("reactionToEmoji(%q) returned empty string", name)
		}

		// Known emojis should return a different string (unicode) than the input.
		knownEmojis := map[string]bool{
			"+1": true, "-1": true, "heart": true, "smile": true,
			"fire": true, "rocket": true, "eyes": true,
		}
		if knownEmojis[name] && result == name {
			t.Errorf("known emoji %q should map to unicode, but got same string back", name)
		}
	})
}

// ---------------------------------------------------------------------------
// FuzzEmojiToReaction — tests the Unicode emoji → kChat name conversion.
// Verifies custom colon-stripping logic doesn't panic on arbitrary input.
// ---------------------------------------------------------------------------

func FuzzEmojiToReaction(f *testing.F) {
	f.Add("\U0001f44d")   // thumbsup
	f.Add("\u2764\ufe0f") // heart
	f.Add(":custom_emoji:")
	f.Add("")
	f.Add(":")
	f.Add("::")
	f.Add(":::")
	f.Add(string([]byte{0x00}))
	f.Add(":a:")
	f.Add("not_an_emoji")

	f.Fuzz(func(t *testing.T, emoji string) {
		result := emojiToReaction(emoji)

		// Determinism check.
		result2 := emojiToReaction(emoji)
		if result != result2 {
			t.Errorf("non-deterministic: emojiToReaction(%q) returned %q then %q",
				emoji, result, result2)
		}

		// Colon-stripping invariant: if input is ":X:" with len > 2,
		// result should be "X" (the inner part without colons).
		if len(emoji) > 2 && emoji[0] == ':' && emoji[len(emoji)-1] == ':' {
			expected := emoji[1 : len(emoji)-1]
			if result != expected {
				t.Errorf("emojiToReaction(%q) = %q, want %q (colon stripping)", emoji, result, expected)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// FuzzEmojiRoundTrip — for known emoji names, verifies the round-trip:
// name → emoji → name should be identity.
// ---------------------------------------------------------------------------

func FuzzEmojiRoundTrip(f *testing.F) {
	// Seed with all known emoji names from the map.
	knownNames := []string{
		"+1", "-1", "heart", "smile", "laughing", "thumbsup", "thumbsdown",
		"wave", "clap", "fire", "100", "tada", "eyes", "thinking",
		"white_check_mark", "x", "warning", "rocket", "star", "pray",
	}

	for _, name := range knownNames {
		f.Add(name)
	}

	f.Fuzz(func(t *testing.T, name string) {
		emoji := reactionToEmoji(name)
		backToName := emojiToReaction(emoji)

		// For known emoji names, the round trip should be identity.
		// Note: "thumbsup" and "+1" both map to the same emoji, so
		// thumbsup → 👍 → +1, not back to thumbsup. Same for thumbsdown/-1.
		// We only check names that have a 1:1 mapping.
		synonyms := map[string]string{
			"thumbsup":   "+1",
			"thumbsdown": "-1",
		}
		expected := name
		if syn, ok := synonyms[name]; ok {
			expected = syn
		}

		// Only assert round-trip for known emoji names.
		knownSet := map[string]bool{
			"+1": true, "-1": true, "heart": true, "smile": true,
			"laughing": true, "thumbsup": true, "thumbsdown": true,
			"wave": true, "clap": true, "fire": true, "100": true,
			"tada": true, "eyes": true, "thinking": true,
			"white_check_mark": true, "x": true, "warning": true,
			"rocket": true, "star": true, "pray": true,
		}
		if knownSet[name] && backToName != expected {
			t.Errorf("round trip failed: %q → %q → %q, want %q",
				name, emoji, backToName, expected)
		}
	})
}

// ---------------------------------------------------------------------------
// FuzzParsePostedEventJSON — feeds arbitrary strings as the JSON payload to
// parsePostedEvent. Must never panic. Returns either a valid post or an error.
// ---------------------------------------------------------------------------

func FuzzParsePostedEventJSON(f *testing.F) {
	// Valid post JSON.
	validPost, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1", Message: "hello",
	})
	f.Add(string(validPost))

	// Malformed JSON.
	f.Add("{bad json")
	f.Add("")
	f.Add("{}")
	f.Add("null")
	f.Add(`{"id": "p1", "user_id": "other-user"}`)
	f.Add(string([]byte{0x00, 0x01, 0x02}))

	// JSON with unexpected types.
	f.Add(`{"id": 123, "user_id": true}`)
	f.Add(`{"message": null}`)

	f.Fuzz(func(t *testing.T, postJSON string) {
		mc := newFullTestClient("http://localhost")
		evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
			"post":        postJSON,
			"sender_name": "@normaluser",
		})

		post, err := mc.parsePostedEvent(evt)

		// Must never get both a post and an error.
		if post != nil && err != nil {
			t.Errorf("parsePostedEvent returned both post and error: post=%+v, err=%v", post, err)
		}
	})
}

// ---------------------------------------------------------------------------
// FuzzParseReactionEventJSON — feeds arbitrary strings as reaction JSON to
// parseReactionEvent. Must never panic.
// ---------------------------------------------------------------------------

func FuzzParseReactionEventJSON(f *testing.F) {
	validReaction, _ := json.Marshal(&model.Reaction{
		UserId: "other-user", PostId: "p1", EmojiName: "+1",
	})
	f.Add(string(validReaction))
	f.Add("{bad json")
	f.Add("")
	f.Add("{}")
	f.Add("null")
	f.Add(string([]byte{0x00}))
	f.Add(`{"user_id": "u1", "post_id": "p1", "emoji_name": "fire"}`)

	f.Fuzz(func(t *testing.T, reactionJSON string) {
		mc := newFullTestClient("http://localhost")
		evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "ch1", map[string]any{
			"reaction": reactionJSON,
		})

		reaction, err := mc.parseReactionEvent(evt)

		// Must never get both a reaction and an error.
		if reaction != nil && err != nil {
			t.Errorf("parseReactionEvent returned both reaction and error: reaction=%+v, err=%v",
				reaction, err)
		}
	})
}

// ---------------------------------------------------------------------------
// FuzzMakeMessagePartID — tests int → PartID conversion. Must never panic
// for any non-negative int. Documents that index >= 10 produces wrong output.
// ---------------------------------------------------------------------------

func FuzzMakeMessagePartID(f *testing.F) {
	f.Add(0)
	f.Add(1)
	f.Add(9)
	f.Add(10)
	f.Add(100)
	f.Add(-1)

	f.Fuzz(func(t *testing.T, index int) {
		// Negative indices are not expected in production but should not panic.
		result := MakeMessagePartID(index)

		// Index 0 must always return empty string.
		if index == 0 && string(result) != "" {
			t.Errorf("MakeMessagePartID(0) = %q, want empty", result)
		}

		// For index 1-9, result should be the ASCII digit.
		if index >= 1 && index <= 9 {
			expected := string(rune('0' + index))
			if string(result) != expected {
				t.Errorf("MakeMessagePartID(%d) = %q, want %q", index, result, expected)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// FuzzFormatDisplayname — tests template rendering with arbitrary parameters.
// Must never panic (template errors fall back to Username).
// ---------------------------------------------------------------------------

func FuzzFormatDisplayname(f *testing.F) {
	f.Add("alice", "Alice N.", "Alice", "Wonderland", "{{.Username}}")
	f.Add("bob", "", "", "", "{{.FirstName}} {{.LastName}}")
	f.Add("", "", "", "", "")
	f.Add("user", "nick", "first", "last", "{{.Nickname}}")
	f.Add(string([]byte{0x00}), "nick", "a", "b", "{{.Username}}")

	f.Fuzz(func(t *testing.T, username, nickname, firstName, lastName, tmpl string) {
		cfg := &Config{DisplaynameTemplate: tmpl}
		// PostProcess parses the template. If it fails, that's fine — we test
		// FormatDisplayname anyway since it should handle nil template gracefully.
		_ = cfg.PostProcess()

		params := DisplaynameParams{
			Username:  username,
			Nickname:  nickname,
			FirstName: firstName,
			LastName:  lastName,
		}

		result := cfg.FormatDisplayname(params)

		// Must always return something (at minimum the username fallback).
		// Note: if username is empty and template fails/is nil, result can be "".
		if cfg.displaynameTemplate == nil && result != username {
			t.Errorf("nil template should return username %q, got %q", username, result)
		}
	})
}

// ---------------------------------------------------------------------------
// FuzzMatrixFmtParse — fuzz the Matrix HTML → kChat markdown converter.
// Feeds arbitrary HTML content through matrixfmtParse. Must never panic.
// The function always returns a string (even for malformed/malicious input).
// ---------------------------------------------------------------------------

func FuzzMatrixFmtParse(f *testing.F) {
	// Plain text (no HTML format).
	f.Add("hello world", "")
	f.Add("", "")

	// Simple HTML formatting.
	f.Add("bold text", "<strong>bold</strong> text")
	f.Add("italic text", "<em>italic</em> text")
	f.Add("strike text", "<del>strike</del> text")
	f.Add("code", "<code>code</code>")
	f.Add("block", "<pre><code>block</code></pre>")
	f.Add("link", `<a href="https://example.com">link</a>`)
	f.Add("heading", "<h1>heading</h1>")
	f.Add("quote", "<blockquote>quoted</blockquote>")
	f.Add("list", "<ul><li>one</li><li>two</li></ul>")
	f.Add("ordered", "<ol><li>first</li><li>second</li></ol>")
	f.Add("para", "<p>paragraph</p>")
	f.Add("break", "line1<br/>line2")

	// XSS vectors — must not panic and should strip dangerous tags.
	f.Add("xss", `<script>alert(1)</script>`)
	f.Add("xss", `<img onerror=alert(1)>`)
	f.Add("xss", `<img src=x onerror="alert(1)">`)
	f.Add("xss", `<svg onload=alert(1)>`)
	f.Add("xss", `<a href="javascript:alert(1)">click</a>`)
	f.Add("xss", `<iframe src="javascript:alert(1)"></iframe>`)
	f.Add("xss", `<body onload=alert(1)>`)
	f.Add("xss", `<div style="background:url(javascript:alert(1))">`)

	// Deeply nested tags.
	f.Add("nested", "<strong><em><del><code>deep</code></del></em></strong>")
	f.Add("nested", strings.Repeat("<div>", 100)+"deep"+strings.Repeat("</div>", 100))

	// Unclosed and malformed tags.
	f.Add("unclosed", "<strong>no close tag")
	f.Add("unclosed", "<a href=\"https://example.com\">no close")
	f.Add("malformed", "<str ong>bad tag</str ong>")
	f.Add("malformed", "< >empty tag</ >")
	f.Add("malformed", "<>")

	// Null bytes and control characters.
	f.Add("null", string([]byte{0x00}))
	f.Add("control", string([]byte{0x00, 0x01, 0x02, 0x03, 0x7f}))
	f.Add("null-in-tag", "<strong>\x00</strong>")

	// Very long strings.
	f.Add("long", strings.Repeat("a", 1000))
	f.Add("long-html", strings.Repeat("<strong>x</strong>", 200))
	f.Add("long-nested", strings.Repeat("<em>", 500)+strings.Repeat("</em>", 500))

	// Mixed content.
	f.Add("mixed", `<h1>Title</h1><p>Paragraph with <strong>bold</strong> and <em>italic</em>.</p><ul><li>item</li></ul>`)

	f.Fuzz(func(t *testing.T, body, formattedBody string) {
		// Test the HTML path: when FormattedBody is set with FormatHTML.
		if formattedBody != "" {
			content := &event.MessageEventContent{
				MsgType:       event.MsgText,
				Body:          body,
				Format:        event.FormatHTML,
				FormattedBody: formattedBody,
			}
			result := matrixfmtParse(content)

			// Determinism: same input must produce same output.
			result2 := matrixfmtParse(content)
			if result != result2 {
				t.Errorf("non-deterministic: matrixfmtParse returned %q then %q for formattedBody=%q",
					result, result2, formattedBody)
			}
		}

		// Test the plain text path: no Format set, returns Body as-is.
		plainContent := &event.MessageEventContent{
			MsgType: event.MsgText,
			Body:    body,
		}
		plainResult := matrixfmtParse(plainContent)
		if plainResult != body {
			t.Errorf("plain text path: matrixfmtParse returned %q, want body %q", plainResult, body)
		}

		// Test nil content does not panic (handled by matrixfmt.Parse).
		nilResult := matrixfmtParse(nil)
		if nilResult != "" {
			t.Errorf("nil content should return empty string, got %q", nilResult)
		}
	})
}

// ---------------------------------------------------------------------------
// FuzzKChatFmtParse — fuzz the kChat markdown → Matrix HTML converter.
// Feeds arbitrary kChat-flavored markdown through kChatFmtParse.
// Must never panic. Result is always a non-nil *ParsedMessage.
// ---------------------------------------------------------------------------

func FuzzKChatFmtParse(f *testing.F) {
	// Plain text.
	f.Add("hello world")
	f.Add("")

	// Standard kChat markdown.
	f.Add("**bold**")
	f.Add("_italic_")
	f.Add("~~strikethrough~~")
	f.Add("`inline code`")
	f.Add("```\ncode block\n```")
	f.Add("```go\nfunc main() {}\n```")
	f.Add("[link](https://example.com)")
	f.Add("# Heading 1")
	f.Add("## Heading 2")
	f.Add("###### Heading 6")
	f.Add("> blockquote")
	f.Add("- list item 1\n- list item 2")
	f.Add("1. ordered item\n2. second item")
	f.Add("@mention")

	// XSS / dangerous link schemes — javascript: must NOT appear in output.
	f.Add("[click](javascript:alert(1))")
	f.Add("[click](JAVASCRIPT:alert(1))")
	f.Add("[click](  javascript:alert(1))")
	f.Add("[click](data:text/html,<script>alert(1)</script>)")
	f.Add("[click](vbscript:alert(1))")
	f.Add("[safe](https://example.com)")
	f.Add("[safe](http://example.com)")
	f.Add("[safe](mailto:user@example.com)")

	// Deeply nested markdown.
	f.Add("**__~~`nested`~~__**")
	f.Add(strings.Repeat("**", 100) + "deep" + strings.Repeat("**", 100))
	f.Add(strings.Repeat("> ", 50) + "deeply quoted")
	f.Add(strings.Repeat("# ", 20) + "many hashes")

	// Null bytes and control characters.
	f.Add(string([]byte{0x00}))
	f.Add(string([]byte{0x00, 0x01, 0x02, 0x03, 0x7f}))
	f.Add("text\x00with\x00nulls")
	f.Add("**bold\x00null**")

	// Very long strings.
	f.Add(strings.Repeat("a", 1000))
	f.Add(strings.Repeat("**bold** ", 200))
	f.Add(strings.Repeat("[x](https://a.com) ", 200))
	f.Add(strings.Repeat("# heading\n", 200))
	f.Add(strings.Repeat("`code` ", 500))

	// Edge cases in markdown parsing.
	f.Add("****")          // empty bold
	f.Add("~~~~")          // empty strikethrough
	f.Add("``")            // empty inline code
	f.Add("```\n```")      // empty code block
	f.Add("[]()")          // empty link text and href
	f.Add("[]( )")         // empty link text, space href
	f.Add("[text]()")      // text but no href
	f.Add("**unclosed")    // unclosed bold
	f.Add("~~unclosed")    // unclosed strikethrough
	f.Add("`unclosed")     // unclosed code
	f.Add("```\nunclosed") // unclosed code block

	// Mixed content.
	f.Add("# Title\n\n**Bold** paragraph with `code` and [link](https://x.com).\n\n> Quote\n\n- item")

	f.Fuzz(func(t *testing.T, text string) {
		result := kChatFmtParse(text)

		// Must always return a non-nil ParsedMessage.
		if result == nil {
			t.Fatalf("kChatFmtParse(%q) returned nil", text)
		}

		// Determinism: same input must produce same output.
		result2 := kChatFmtParse(text)
		if result2 == nil {
			t.Fatalf("non-deterministic: second call returned nil for %q", text)
		}
		if result.Body != result2.Body || result.FormattedBody != result2.FormattedBody || result.Format != result2.Format {
			t.Errorf("non-deterministic: kChatFmtParse(%q) returned different results:\n  first:  Body=%q Format=%q FormattedBody=%q\n  second: Body=%q Format=%q FormattedBody=%q",
				text, result.Body, result.Format, result.FormattedBody,
				result2.Body, result2.Format, result2.FormattedBody)
		}

		// Security invariant: javascript: scheme must never appear in the
		// formatted HTML output. The formatter strips unsafe URL schemes.
		if result.FormattedBody != "" {
			lower := strings.ToLower(result.FormattedBody)
			if strings.Contains(lower, "javascript:") {
				t.Errorf("formatted output contains javascript: scheme for input %q:\n  FormattedBody=%q",
					text, result.FormattedBody)
			}
		}

		// For non-empty input, Body should preserve the original text.
		if text != "" && result.Body != text && result.Body != "" {
			// When formatting is detected, Body should equal the original text.
			if result.Format == event.FormatHTML && result.Body != text {
				t.Errorf("formatted message Body should equal input: got %q, want %q", result.Body, text)
			}
		}
	})
}
