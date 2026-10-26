# kChat ↔ Beeper nutzen (Team-Onboarding)

Diese Anleitung richtet sich an Mitglieder des kChat-Teams **my-team**.
Damit kann jedes Teammitglied seine kChat-Kanäle und -DMs aus **Beeper** (und damit aus
WhatsApp/Signal/Telegram etc. in derselben App) lesen und beantworten.

## Zwei Varianten

| Variante | Aufwand | Identität in kChat |
| --- | --- | --- |
| **A) Eigene Bridge-Instanz** (empfohlen) | ~10 Minuten einmalig | Eigene Beiträge erscheinen in kChat unter deinem eigenen kChat-Benutzer |
| B) Nur mitlesen | keine | — (ihr könnt bereits alles mitlesen, was in euren kChat-Kanälen passiert) |

Ihr müsst **nichts tun**, um Nachrichten aus kChat-Kanälen zu empfangen — die Bridge von
Roland (`team-member`) relayt bereits die Team-Kanäle. Variante A ist für den Fall, dass
ihr **aus Beeper heraus** mit eurer eigenen kChat-Identität schreiben wollt.

## Voraussetzungen (Variante A)

1. **Beeper-Konto**: [beeper.com](https://www.beeper.com) registrieren (E-Mail genügt)
2. **kChat-Zugang**: euer normaler kChat-Account im Team my-team
3. **Infomaniak-Manager-Zugang**: für euren kChat-API-Token

## Schritt 1: kChat-API-Token erstellen

1. Im Infomaniak Manager die Token-Verwaltung öffnen:
   <https://manager.infomaniak.com/v3/ng/accounts/token/list>
2. **Token erstellen** → als Produkt **kChat** wählen → Gültigkeit wählen
3. Mit dem Kontopasswort bestätigen
4. Token **sofort kopieren** — er wird nur einmal angezeigt

> ⚠️ Den Token niemals weitergeben oder in Chats einfügen. Er gewährt vollen
> Zugriff auf euren kChat-Account. Bei Verlust: Token im Manager widerrufen und neu erstellen.

## Schritt 2: Beeper-Bridge-Konfiguration erzeugen

Auf dem eigenen Rechner (Linux/macOS/WSL, Go 1.25+ nötig):

```bash
# bbctl installieren und einloggen (mit der BEPER-E-Mail)
git clone https://github.com/beeper/bridge-manager.git
cd bridge-manager && ./build.sh
./bbctl login

# Bridge-Konfiguration erzeugen (Name frei wählbar, muss mit sh- beginnen)
./bbctl config --type bridgev2 sh-kchat > kchat-config.yaml
```

Die Datei `kchat-config.yaml` enthält eure persönlichen Beeper-Bridge-Zugangsdaten.

## Schritt 3a: Installation auf dem Team-Server (empfohlen)

Den Server-Admin (Roland) bitten, einen eigenen Container einzurichten — benötigt wird
nur eure `kchat-config.yaml` (z. B. per Passwort-Manager oder persönlich übergeben).

Der Admin führt dann aus:

```bash
# NAME = euer kChat-Benutzername (z. B. example-user)
sudo mkdir -p /opt/kchat-NAME/logs
sudo cp kchat-config.yaml /opt/kchat-NAME/config.yaml
sudo chown -R rosch:rosch /opt/kchat-NAME
sudo docker run -d \
  --name matrix-kchat-NAME \
  --restart unless-stopped \
  -v /opt/kchat-NAME:/data \
  ghcr.io/rosch100/kchat:0.1
```

- Jede Instanz bekommt ein eigenes Datenverzeichnis — Container brauchen **keine Ports**.
- Updates: `sudo docker pull ghcr.io/rosch100/kchat:0.1` und Container neu erstellen.

## Schritt 3b: Installation auf dem eigenen Rechner (Alternative)

Ohne Server geht es auch lokal — die Bridge läuft dann nur, solange der Rechner an ist:

```bash
git clone https://github.com/rosch100/kchat.git
cd kchat
make build        # benötigt libolm (E2EE)
mkdir data && cp ../pfad/zu/kchat-config.yaml data/config.yaml
cd data && ../matrix-kchat -c config.yaml
```

## Schritt 4: Mit kChat verbinden (Login)

1. In **Beeper** einen Chat mit der Bridge-Bot-Adresse öffnen:
   Suchfeld → `@sh-kchatbot:beeper.local`
2. `login` senden
3. Flow **„Personal Access Token"** wählen
4. eure kChat-Server-URL eingeben:
   `https://my-team.kchat.infomaniak.com`
5. euren Token aus Schritt 1 einfügen

Fertig: Die Bridge synct eure kChat-Kanäle und -DMs — ihr seht danach in Beeper genau die
Kanäle, in denen euer kChat-Account Mitglied ist. Neue Nachrichten kommen innerhalb von
~5 Sekunden.

## Verhalten und Grenzen

- **Latenz**: Polling alle 5 Sekunden (kChat broadcastet Beiträge nicht zu Drittanbieter-Sockets)
- **Schreiben**: Nachrichten aus Beeper erscheinen in kChat unter eurem kChat-Benutzer
- **Löschen/Editieren**: funktioniert in beide Richtungen
- **Read-Receipts**: kChat → Beeper (kChat hat keine API für die Gegenrichtung)
- **Admin-API/LLM-Funktionen** der Bridge sind in der Konfiguration deaktiviert (`admin_api_addr: ""`)

## Probleme?

- Bridge-Logs (auf dem Server): `sudo docker logs matrix-kchat-NAME`
- Login schlägt fehl: Token prüfen (richtiger Scope? kChat?) und Token-Verfall prüfen
- Team-Kontakt: Team Admin
