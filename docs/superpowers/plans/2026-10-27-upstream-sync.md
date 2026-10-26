# Upstream-Synchronisation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Regelmäßige Änderungen aus `aiku/mautrix-mattermost` als geprüfte Pull Requests in `kchat` vorschlagen.

**Architecture:** Ein GitHub-Actions-Workflow fetched den explizit konfigurierten Upstream-Branch, berechnet seit einer gespeicherten Baseline einen Drei-Wege-Patch, wendet ihn auf KChat an und erstellt oder aktualisiert einen Pull Request. Konflikte führen zu einem sichtbaren fehlgeschlagenen Lauf; die bestehende CI entscheidet über die technische Verträglichkeit.

**Tech Stack:** GitHub Actions, Git, `peter-evans/create-pull-request`.

## Global Constraints

- Der Upstream ist `https://github.com/aiku/mautrix-mattermost.git`, Branch `main`.
- Die initiale Baseline ist `96283ac8e34181e089dcdc09ffd2bcf22732c9f4` in `.github/upstream-sync-base`.
- Upstream-Änderungen dürfen nicht direkt nach `main` gepusht werden.
- Patch-Konflikte werden nicht automatisch aufgelöst.
- Bestehende CI- und Dependabot-Konfigurationen bleiben unverändert.
- Workflow-Berechtigungen werden auf Branch- und PR-Erstellung begrenzt.

---

### Task 1: Upstream-Sync-Workflow

**Files:**
- Create: `.github/upstream-sync-base`
- Create: `.github/workflows/upstream-sync.yml`

**Interfaces:**
- Consumes: `main` des aktuellen Repositorys, die Baseline-Datei und `main` von `aiku/mautrix-mattermost`.
- Produces: Branch `sync/upstream-mautrix-mattermost` und einen passenden Pull Request.

- [ ] **Step 1: Write the workflow**

Der Workflow enthält:

```yaml
name: Upstream sync

on:
  schedule:
    - cron: '17 4 * * 1'
  workflow_dispatch:

permissions:
  contents: write
  pull-requests: write

jobs:
  sync:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - name: Fetch upstream
        run: git fetch --no-tags https://github.com/aiku/mautrix-mattermost.git main:refs/remotes/upstream/main
      - name: Apply changes since the last successful sync
        shell: bash
        run: |
          set -euo pipefail
          base="$(tr -d '[:space:]' < .github/upstream-sync-base)"
          upstream="$(git rev-parse refs/remotes/upstream/main)"
          git cat-file -e "$base^{commit}"
          git diff --binary "$base" "$upstream" > /tmp/upstream.patch
          git apply --3way --index /tmp/upstream.patch
          printf '%s\n' "$upstream" > .github/upstream-sync-base
      - name: Create sync pull request
        uses: peter-evans/create-pull-request@v7
        with:
          branch: sync/upstream-mautrix-mattermost
          delete-branch: true
          commit-message: "chore: sync aiku/mautrix-mattermost"
          title: "chore: sync aiku/mautrix-mattermost"
          body: |
            Aktualisiert KChat mit Änderungen aus `aiku/mautrix-mattermost`.

            Dieser PR muss auf kChat-spezifische Konflikte und Verhaltensänderungen
            geprüft werden. Die regulären CI-Checks müssen erfolgreich sein.
```

- [ ] **Step 2: Validate workflow structure**

Run:

```bash
git diff --check
```

Expected: no output and exit code 0.

- [ ] **Step 3: Run repository validation**

Run:

```bash
go test ./...
go vet ./...
go build ./...
```

Expected: all commands exit with code 0.

- [ ] **Step 4: Review the resulting diff**

Run:

```bash
git diff -- .github/workflows/upstream-sync.yml
```

Expected: only the new workflow, its baseline file and the documentation files
are present.
