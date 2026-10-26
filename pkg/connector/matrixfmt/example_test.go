// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package matrixfmt_test

import (
	"fmt"

	"maunium.net/go/mautrix/event"

	"github.com/rosch100/kchat/pkg/connector/matrixfmt"
)

func ExampleParse() {
	content := &event.MessageEventContent{
		MsgType:       event.MsgText,
		Body:          "hello world",
		Format:        event.FormatHTML,
		FormattedBody: "<strong>hello</strong> <em>world</em>",
	}

	md := matrixfmt.Parse(content)
	fmt.Println(md)
	// Output: **hello** _world_
}
