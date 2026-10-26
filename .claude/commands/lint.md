Run all linting and static analysis checks on the codebase.

1. Run `go vet ./...`
2. Run `gofmt -l .` to check for formatting issues
3. Run `golangci-lint run ./...`

Report any issues found, grouped by severity. For each issue, show the file, line, and a brief fix suggestion.

4. **Stamp file**: If all checks pass with zero issues, create the lint stamp by running:
   ```
   touch .claude/lint-stamp
   ```
   This stamp is checked by the pre-commit hook — commits are blocked until the stamp exists and is newer than all `.go` source files. If any check fails, do NOT create the stamp.
