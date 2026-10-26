Review the current codebase or staged changes for security issues using the security-reviewer agent.

Follow the Go Security Agent prompt in full:

1. Run all security and lint tools in order:
   ```
   go vet ./...
   ```
   ```
   golangci-lint run ./...
   ```
   ```
   gosec ./...
   ```
   ```
   govulncheck ./...
   ```
   ```
   make test-race
   ```

2. If there are staged or uncommitted changes, run `git diff` and `git diff --cached` to scope the review.

3. Produce the full structured review:
   - Tool results summary
   - STRIDE assessment (all 6 categories with explicit N/A/PASS/FINDING)
   - OWASP Top 10:2025 mapping for every finding
   - Dependency changes audit (if go.mod/go.sum changed)
   - Findings ordered by severity with file:line, CWE, OWASP category, concrete fix
   - Final verdict: APPROVE / REQUEST CHANGES / BLOCKING

4. Pay special attention to project-specific security surfaces:
   - Echo prevention layer integrity (5 layers, never weaken)
   - Puppet token handling (never log tokens)
   - Admin API authentication (`POST /api/reload-puppets`)
   - WebSocket event parsing (untrusted input from kChat)
   - Format conversion (user-generated content parsing)

When reviewing or writing security-related code, follow the full standards in `.claude/agents/security-reviewer.md`.

5. **Stamp files**: If the final verdict is **APPROVE**, create both stamp files by running:
   ```
   touch .claude/security-review-stamp
   touch .claude/lint-stamp
   ```
   These stamps are checked by pre-commit hooks — commits are blocked until both stamps exist and are newer than all `.go` source files. If the verdict is REQUEST CHANGES or BLOCKING, do NOT create either stamp.
