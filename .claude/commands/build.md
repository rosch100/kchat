Build the bridge binary and verify it compiles cleanly.

1. Run `go vet ./...` first to catch issues early
2. Run `go build ./cmd/matrix-kchat/` to build the binary
3. Report success or failure. On failure, analyze the compiler errors and suggest fixes.
