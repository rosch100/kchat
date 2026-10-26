# Contributing to matrix-kchat

Thanks for your interest in contributing! This project is a Matrix bridge for
Infomaniak kChat built on the [mautrix bridgev2](https://github.com/mautrix/go)
framework, derived from [aiku/mautrix-mattermost](https://github.com/aiku/mautrix-mattermost).

## Getting started

1. Fork the repository and create your branch from `main`.
2. Install prerequisites: Go 1.25+, libolm for E2EE
   (`brew install ...` — see the README's build notes).
3. Build and run the tests:

```bash
go build ./...
go test ./...
```

## Before opening a PR

- Run `gofmt -s -w .` and `go vet ./...` — CI enforces both plus the test suite.
- Add or update tests for any behavior change. The connector package has unit
  tests with a fake API server (`pkg/connector/testhelpers_test.go`) to build on.
- Keep imports at the top of files; no inline imports.
- New events handled in `pkg/connector/handlekchat.go` should follow the
  existing echo-prevention patterns (see `pkg/connector/echotracker.go`).

## Reporting issues

Use the issue templates. For bugs, include the bridge log
(`logs/bridge.log`, level `debug`) with credentials redacted — never paste
access tokens.

## License

By contributing, you agree that your contributions are licensed under the
[MPL-2.0](LICENSE), same as the rest of the project.
