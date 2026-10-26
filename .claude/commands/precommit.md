Run the full pre-commit validation pipeline before committing.

Execute these steps in order, stopping on first failure:

1. **Format**: Run `gofmt -l .` — if any files are unformatted, run `gofmt -s -w .` to fix them
2. **Vet**: Run `go vet ./...`
3. **Test**: Run `go test -race ./... -v`
4. **Build**: Run `go build ./cmd/matrix-kchat/`

Report a pass/fail summary for each step. If everything passes, confirm the code is ready to commit.
