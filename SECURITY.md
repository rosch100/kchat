# Security Policy

## Supported versions

Only the latest release on `main` (and tags cut from it) receives security fixes.

## Reporting a vulnerability

Please do **not** report security vulnerabilities through public GitHub issues.

Instead:

1. Use [GitHub's private vulnerability reporting](https://github.com/rosch100/kchat/security/advisories/new)
   on this repository, **or**
2. Open a GitHub issue asking for a private channel, and we will follow up.

## Scope

- The bridge binary and its configuration handling
- Credential storage (`config.yaml`, `user_login` metadata)
- The realtime socket and kChat API client (`pkg/connector`)

## Notes on credentials

Bridge configuration files contain appservice tokens and kChat access tokens.
Never attach real configuration files or logs with tokens to public reports;
redact them first.
