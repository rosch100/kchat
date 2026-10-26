Look up kChat or mautrix documentation using the kchat-expert agent.

Use context7 as the primary source with these library IDs:
- `/kchat/docs` — MM platform docs
- `/websites/developers_kchat_api-documentation` — REST API reference
- `/kchat/kchat` — MM source code and Go types
- `/mautrix/go` — mautrix Go framework and bridgev2
- `/websites/mau_fi_bridges` — Bridge setup and configuration docs

Fall back to firecrawl search against `developers.kchat.com`, `docs.mau.fi`, or `pkg.go.dev` if context7 doesn't cover it.

After finding the answer, cross-reference with the local codebase in `pkg/connector/` to show how it's currently used in this project.

Answer the user's question with: the answer, the source, local context, and any caveats.
