// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package kchatfmt_test

import (
	"fmt"

	"git.altanis.de/rosch/kchat/pkg/connector/kchatfmt"
)

func ExampleParse() {
	msg := kchatfmt.Parse("**hello** world")
	fmt.Println(msg.FormattedBody)
	// Output: <strong>hello</strong> world
}
