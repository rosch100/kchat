# Upstream-Synchronisation für mautrix-mattermost

## Ziel

Änderungen aus `aiku/mautrix-mattermost` sollen regelmäßig erkannt und als
prüfbarer Pull Request gegen `main` vorgeschlagen werden. Änderungen dürfen
nicht ungeprüft direkt in `main` oder in ein Release gelangen.

## Design

Der Workflow `.github/workflows/upstream-sync.yml` läuft wöchentlich und
manuell. Er verwendet `aiku/mautrix-mattermost` auf dessen `main`-Branch als
Upstream. Da KChat keine gemeinsame Git-Historie mit dem abgeleiteten Repository
besitzt und zentrale Dateien kChat-spezifisch umgebaut wurden, wird kein
unabhängiger Git-Merge verwendet. Stattdessen wird der Unterschied zwischen dem
zuletzt synchronisierten Upstream-Commit und dem aktuellen Upstream-Commit als
Drei-Wege-Patch auf KChat angewendet.

Der zuletzt erfolgreich synchronisierte Upstream-Commit steht in
`.github/upstream-sync-base`. Der Workflow beendet sich erfolgreich, wenn kein
neuer Upstream-Commit vorliegt. Bei Patch-Konflikten wird kein PR mit
unvollständigem Inhalt erstellt; der Workflow schlägt mit einer verständlichen
Meldung fehl. Dadurch bleibt die manuelle Anpassung kChat-spezifischer
Änderungen sichtbar.

Der PR läuft durch die bereits vorhandenen CI- und Lint-Workflows. Der
Sync-Workflow erhält nur die für Branch- und PR-Erstellung notwendigen
Repository-Berechtigungen. Dependency-Updates über `go.mod` bleiben dem
bestehenden Dependabot-Workflow überlassen.

## Datenfluss

1. Checkout von `main`.
2. Fetch von `aiku/mautrix-mattermost/main`.
3. Lesen des letzten synchronisierten Commits.
4. Erzeugen und Anwenden eines Drei-Wege-Patches.
5. Aktualisieren der Baseline-Datei.
6. Push des Sync-Branches.
7. Erstellung oder Aktualisierung eines Pull Requests.

Der Patch wird absichtlich mit `git apply --3way` angewendet: Upstream-Code soll
tatsächlich sichtbar werden; Konflikte sollen den Prozess stoppen.

## Fehlerbehandlung und Sicherheit

- Fehlende Upstream-Erreichbarkeit beendet den Workflow mit Fehler.
- Patch-Konflikte werden abgebrochen und hinterlassen keinen falschen grünen PR.
- Der Pull Request ist der einzige Schreibkanal in das Repository.
- Der Workflow nutzt `pull-requests: write` und `contents: write`; keine
  Secrets oder Tokens werden in Dateien geschrieben.
- Der Upstream-Branch und die URL sind im Workflow explizit festgelegt.

## Akzeptanzkriterien

- Der Workflow kann per Zeitplan und `workflow_dispatch` gestartet werden.
- Kein neuer Upstream-Stand erzeugt keinen Commit und keinen unnötigen PR.
- Ein neuer Upstream-Stand erzeugt einen PR mit dem Sync-Branch und aktualisierter
  Baseline.
- Ein nicht automatisch lösbarer Konflikt führt zu einem fehlgeschlagenen Lauf.
- YAML-Linting sowie die vorhandenen Go-Checks bleiben unverändert.
