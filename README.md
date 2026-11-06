# printer — agent d'impression de la caisse

Agent d'impression des points de vente Chapitre Neuf : il récupère les jobs de l'API (Mercure SSE + polling de
secours) et imprime tickets ESC/POS, étiquettes et stickers, ouvre le tiroir-caisse.

Tout tient dans **un seul binaire, `printer`** : l'agent lui-même (`printer run`, lancé par systemd) et les commandes
d'exploitation (`printer status`, `printer log`, `printer update`…). Il tourne en arrière-plan comme service systemd
*utilisateur*, démarre au boot sans session ouverte (linger) et se met à jour depuis les GitHub Releases.

| Emplacement | Rôle |
|---|---|
| `~/.local/bin/printer` | le binaire (`printer.prev` : version précédente, pour `printer rollback`) |
| `~/.local/bin/print-agent` | lien vers `printer` (compatibilité avec l'ancienne installation) |
| `~/.config/print-agent/env` | la configuration (identifiants API, imprimantes, adresse du magasin), droits `600` |
| `~/.config/systemd/user/print-agent.service` | le service (réécrit par `printer setup`, ne pas l'éditer) |
| `~/.config/systemd/user/printer-update.{service,timer}` | la mise à jour automatique (désactivée par défaut) |

## Installation sur un PC neuf

Pré-requis : Ubuntu (ou dérivé) avec systemd, `curl`, l'utilisateur de la caisse. **Pas besoin de Go.**

```sh
# en tant qu'utilisateur de la caisse (pas root, pas sudo)
curl -fsSL https://raw.githubusercontent.com/ICTools/printer-agent/main/deploy/install.sh | bash
```

Le script télécharge le binaire de la dernière release pour le processeur du PC (`amd64` ou `arm64`), vérifie son
sha256 contre `checksums.txt`, puis lance `printer setup`, qui :

1. installe `~/.local/bin/printer` ;
2. crée `~/.config/print-agent/env` depuis le modèle **s'il n'existe pas** (une config existante n'est jamais écrasée) ;
3. installe les unités systemd utilisateur et active le service au boot (dès que la config est valide) ;
4. active le *linger* (`loginctl enable-linger`) : le service démarre au boot sans que personne ne se connecte ;
5. vérifie le PATH, l'accès aux imprimantes, `python3` et `brother_ql` ;
6. démarre le service si la config est remplie.

Ensuite :

```sh
printer config edit     # renseigner PRINT_AGENT_API_URL / _KEY / _SECRET ; valide puis redémarre
printer status
printer test            # imprime un ticket de test
```

Si `printer` n'est pas trouvé, ouvrir un nouveau terminal (Ubuntu ajoute `~/.local/bin` au PATH à la connexion) ou
utiliser `~/.local/bin/printer`.

Options : une version précise `curl -fsSL …/install.sh | bash -s -- --version v1.2.0`.
Relancer le script (ou `printer setup`) est sans risque : il répare l'installation et garde la config.

### Dépendances système

```sh
sudo usermod -aG lp $USER          # accès à /dev/usb/lp* (puis redémarrer la session / le PC)
sudo apt install python3-pil       # étiquettes et stickers
python3 -m pip install brother_ql  # étiquettes Brother QL (ou pipx install brother_ql)
```

Les scripts Python d'étiquettes (`scripts/print.py`, `scripts/print_sticker.py`) sont **embarqués dans le binaire** et
extraits au besoin dans `~/.cache/print-agent/scripts/` ; `LABEL_SCRIPT_PATH` / `STICKER_SCRIPT_PATH` restent
prioritaires s'ils sont définis.

### Dépôt privé

Le dépôt est public aujourd'hui : rien à faire. S'il devient privé, les releases demandent une authentification :

- installation : `raw.githubusercontent.com` ne sert plus le script ; avec `gh` connecté (`gh auth login`) :
  `gh api repos/ICTools/printer-agent/contents/deploy/install.sh -H 'Accept: application/vnd.github.raw' | bash`.
  Le script bascule tout seul sur `gh release download` quand le téléchargement public échoue (`gh` utilise
  `GH_TOKEN`/`GITHUB_TOKEN` s'ils sont définis) ;
- `printer update` (manuel ou automatique) prend, dans l'ordre : `PRINTER_GITHUB_TOKEN`, `GH_TOKEN`, `GITHUB_TOKEN`
  (environnement, ou ligne `PRINTER_GITHUB_TOKEN=…` dans `~/.config/print-agent/env`), puis le token de `gh`. Un
  token *fine-grained* en lecture seule sur ce seul dépôt (Contents: read) suffit.

## Commandes

| Commande | Effet |
|---|---|
| `printer status` | version, état du service, démarrage au boot, linger, config, imprimantes, derniers logs |
| `printer start` / `stop` / `restart` | pilote le service ; `start`/`restart` refusent une config invalide et vérifient que l'agent reste démarré |
| `printer log` | suit les logs en temps réel (50 dernières lignes puis la suite) |
| `printer log -n 200` | les 200 dernières lignes, sans suivre (`-f` pour suivre aussi) |
| `printer log --since today --errors` | erreurs du jour (`--since "1 hour ago"`, `--since 2026-11-06`…) |
| `printer update --check` | compare la version installée à la dernière release |
| `printer update` | installe la dernière release (voir ci-dessous) |
| `printer update --version v1.2.0` | installe une version précise (aussi pour revenir en arrière) |
| `printer rollback` | revient au binaire remplacé par la dernière mise à jour (et inversement) |
| `printer version` | version, commit, date de build (`--short` : juste la version) |
| `printer config` | affiche la config, secrets masqués (`PRINT_AGENT_API_SECRET=********`, clé API tronquée) |
| `printer config edit` | ouvre `$VISUAL`/`$EDITOR`/nano, valide, propose de corriger, puis active et redémarre le service |
| `printer config check` / `path` | valide la config / affiche son chemin |
| `printer test` | imprime un ticket « TEST IMPRESSION » (version, poste, date, device) sur l'imprimante ticket, sans tiroir |
| `printer autoupdate on` / `off` / `status` | mise à jour automatique quotidienne |
| `printer setup` | installe ou répare tout (idempotent) |
| `printer run` | l'agent au premier plan (ce que lance systemd) — options : `printer run --help` |
| `printer detect`, `receipt-test`, `open-drawer`, `label`, `sticker-address`, `sticker-image` | outils d'impression manuels (voir « Mode CLI ») |

## Mise à jour

```sh
printer update --check
printer update
```

`printer update` :

1. interroge l'API GitHub (`releases/latest`) et compare les versions (semver ; un build de dev est toujours remplacé) ;
2. télécharge `printer_linux_<arch>` et `checksums.txt`, **vérifie le sha256** (rien n'est installé sinon) ;
3. exécute le nouveau binaire (`version --short`) pour vérifier qu'il tourne et annonce la bonne version ;
4. remplace `~/.local/bin/printer` par un `rename` **atomique**, l'ancien est gardé en `printer.prev` ;
5. si le service tournait, le redémarre et vérifie pendant 20 s (`--wait`) qu'il reste actif sans redémarrage
   automatique de systemd ;
6. sinon **rollback** : remet l'ancien binaire et redémarre. Si l'ancien ne tient pas non plus, le problème n'est pas
   la mise à jour (réseau, API, config) : voir `printer log -n 100`.

Un verrou empêche deux mises à jour simultanées. Si les unités systemd changent entre deux versions, les notes de
release le disent : lancer alors `printer setup`.

### Mise à jour automatique (désactivée par défaut)

```sh
printer autoupdate on      # tous les jours vers 05:00 (+ jusqu'à 30 min aléatoires)
printer autoupdate off
journalctl --user -u printer-update.service   # historique
```

Désactivée par défaut : une mise à jour redémarre l'agent, et une caisse doit changer de version quand on l'a décidé
(après une release testée). Le timer n'est pas *persistent* : un PC éteint à 05:00 n'est pas mis à jour au démarrage,
en pleine ouverture. Le rollback automatique s'applique aussi.

## Logs

Les logs vont dans journald (rotation gérée par journald) :

```sh
printer log                         # suivi temps réel
printer log -n 100                  # 100 dernières lignes
printer log --since today --errors  # erreurs du jour
journalctl --user -u print-agent --since "2026-11-06 09:00" --until "2026-11-06 10:00"
```

## Dépannage

| Symptôme | Piste |
|---|---|
| `printer: command not found` | nouveau terminal, ou `~/.local/bin/printer` |
| `status` : `activating (auto-restart)`, restarts qui montent | l'agent quitte au démarrage : `printer log -n 50` ; souvent identifiants refusés ou API injoignable (`printer config check`) |
| `Failed to connect to bus` / `systemctl --user` échoue | lancer les commandes en session normale de l'utilisateur de la caisse, pas via `sudo` |
| le service ne démarre qu'après connexion | linger off : `printer setup` (ou `sudo loginctl enable-linger $USER`) |
| `Permission denied` sur `/dev/usb/lp0` | `sudo usermod -aG lp $USER` puis redémarrer ; `printer status` liste les devices et leur accès |
| aucune imprimante détectée | câble / alimentation ; `printer detect` |
| `brother_ql not found` | installer `brother_ql` (voir Dépendances) ; vérifier `PATH`/`PYTHON_PATH` |
| `update` : `checksum mismatch` | téléchargement corrompu ou release modifiée : rien n'a été installé, relancer ; sinon vérifier la release |
| `update` : `no matching release` | aucune release publiée, ou dépôt privé sans token (voir « Dépôt privé ») |
| une mise à jour pose problème | `printer rollback`, ou `printer update --version vX.Y.Z` |
| texte illisible sur le ticket | les accents sont convertis en ASCII (normal) |
| TLS en local | `printer run -insecure` |

## Migration depuis l'ancienne installation

Ancienne installation (`deploy/install.sh` d'avant, build Go local) : binaire `~/.local/bin/print-agent`, service
`print-agent`, config `~/.config/print-agent/env`. Le nouveau binaire garde **le même nom de service et la même
config**, la migration est donc :

```sh
cp ~/.config/print-agent/env ~/print-agent-env.backup     # sécurité
curl -fsSL https://raw.githubusercontent.com/ICTools/printer-agent/main/deploy/install.sh | bash
printer status          # active (running), config OK
printer test            # ticket de test
```

`printer setup` (lancé par le script) : garde `~/.config/print-agent/env` tel quel (droits passés à `600`), réécrit
`print-agent.service` avec `ExecStart=%h/.local/bin/printer run`, déplace l'ancien binaire en
`~/.local/bin/print-agent.legacy` et le remplace par un lien vers `printer`, puis redémarre le service. Le clone Go
local n'est plus nécessaire.

Retour arrière (si besoin) : `mv ~/.local/bin/print-agent.legacy ~/.local/bin/print-agent` (remplace le lien), remettre
`ExecStart=%h/.local/bin/print-agent run` dans `~/.config/systemd/user/print-agent.service`, puis
`systemctl --user daemon-reload && systemctl --user restart print-agent`.

## Désinstallation

```sh
systemctl --user disable --now print-agent printer-update.timer
rm ~/.config/systemd/user/print-agent.service ~/.config/systemd/user/printer-update.*
systemctl --user daemon-reload
rm ~/.local/bin/printer ~/.local/bin/printer.prev ~/.local/bin/print-agent
# la config ~/.config/print-agent/env contient les identifiants : la supprimer si le poste est retiré
```

## Développement

```sh
go vet ./... && go test ./...
go build -o printer ./cmd/printer
./deploy/install.sh --from-source     # installe le build local (version = git describe)
```

### Publier une release

Les releases sont produites par `.github/workflows/release.yml` sur un tag `v*` : tests, build `linux/amd64` et
`linux/arm64` (statiques, `CGO_ENABLED=0`, version injectée par `-ldflags -X main.version`), `checksums.txt`, puis
GitHub Release avec notes générées. Un tag `vX.Y.Z-rc.1` crée une pré-release (ignorée par `printer update`).

```sh
git checkout main && git pull
git tag -a v1.0.0 -m "v1.0.0"
git push origin v1.0.0
```

---

## Mode Agent (`printer run`)

Ce que lance le service. À la main (tests) :

```sh
printer run -api-url https://api.monsite.com -api-key $API_KEY -api-secret $API_SECRET
```

### Options

| Flag | Variable d'env | Défaut | Description |
|------|----------------|--------|-------------|
| `-api-url` | `PRINT_AGENT_API_URL` | - | URL de l'API (requis) |
| `-api-key` | `PRINT_AGENT_API_KEY` | - | Clé API (requis) |
| `-api-secret` | `PRINT_AGENT_API_SECRET` | - | Secret API (requis) |
| `-poll-interval` | `PRINT_AGENT_POLL_INTERVAL` | `2s` | Intervalle polling jobs |
| `-ping-interval` | `PRINT_AGENT_PING_INTERVAL` | `30s` | Intervalle heartbeat |
| `-sync-interval` | `PRINT_AGENT_SYNC_INTERVAL` | `10s` | Intervalle détection imprimantes |
| `-health-addr` | `PRINT_AGENT_HEALTH_ADDR` | - | Serveur health (ex: `:8080`) |
| `-timeout` | - | `30s` | Timeout HTTP |
| `-verbose` | - | `false` | Logs détaillés |
| `-dry-run` | - | `false` | Log jobs sans exécuter |
| `-insecure` | - | `false` | Désactive vérification TLS |
| `-disable-sse` | - | `false` | Force le mode polling (désactive Mercure SSE) |

### Test local (sans imprimante)

```sh
printer run \
  -api-url https://localhost \
  -api-key $API_KEY \
  -api-secret $API_SECRET \
  -insecure \
  -dry-run \
  -verbose
```

---

## Architecture

L'agent supporte deux modes de récupération des jobs :

### Mode Event-Driven (SSE/Mercure) - Recommandé

Si le serveur supporte Mercure, l'agent utilise Server-Sent Events pour recevoir les notifications en temps réel.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              SERVEUR (Symfony + FrankenPHP)                 │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────────────────────┐   │
│  │   POS/App   │────▶│ PrintJob    │────▶│      Hub Mercure            │   │
│  │             │     │ Service     │     │  (FrankenPHP intégré)       │   │
│  └─────────────┘     └─────────────┘     └──────────────┬──────────────┘   │
│        │                   │                            │                   │
│        │              Crée job +                   SSE Push                 │
│        │              Publie event             (notification)               │
│        │                   │                            │                   │
│        │                   ▼                            │                   │
│        │             ┌─────────────┐                    │                   │
│        │             │  Base de    │                    │                   │
│        │             │  données    │                    │                   │
│        │             └─────────────┘                    │                   │
└────────│────────────────────▲───────────────────────────│───────────────────┘
         │                    │                           │
         │                    │ GET /jobs/next            │
         │                    │ (fetch complet)           │
         │                    │                           ▼
┌────────│────────────────────│───────────────────────────────────────────────┐
│        │                    │                     ┌─────────────┐           │
│        │                    └─────────────────────│  SSE Loop   │◀──────────│
│        │                                          │  (Mercure)  │           │
│        │    ┌─────────────┐     ┌─────────────┐  └──────┬──────┘           │
│        │    │  Dispatcher │◀────│  Poll Loop  │◀────────┘                  │
│        │    │             │     │  (fallback) │    notification            │
│        │    └──────┬──────┘     └─────────────┘                            │
│        │           │                                                        │
│        │           ▼                                                        │
│        │    ┌─────────────┐                                                 │
│        │    │ Imprimantes │                                                 │
│        │    │ (USB/Réseau)│                                                 │
│        │    └─────────────┘                                                 │
│                           AGENT (Go)                                        │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Flux :**

1. Le POS crée une vente → `PrintJobService` crée un job en base
2. `PrintJobService` publie une notification sur Mercure : `{event: "new_job", job_id, type, printer_code}`
3. L'agent reçoit la notification via SSE (connexion persistante)
4. L'agent fait immédiatement `GET /jobs/next` pour récupérer le job complet
5. Le job est exécuté et acquitté via `POST /jobs/{id}/ack`

**Sécurité :**

- Chaque agent a un topic privé : `/printer-agent/{agent-id}/jobs`
- Le JWT Mercure restreint l'accès au seul topic de l'agent
- Seules les métadonnées transitent par SSE (pas de données sensibles)
- Le payload complet est récupéré via HTTPS (API REST)

### Mode Polling (fallback)

Si Mercure n'est pas disponible ou désactivé (`--disable-sse`), l'agent poll l'API toutes les 2 secondes.

```
┌─────────────────┐                      ┌─────────────────┐
│     SERVEUR     │                      │      AGENT      │
│                 │  GET /jobs/next      │                 │
│   ┌─────────┐   │◀─────────────────────│   ┌─────────┐   │
│   │  Jobs   │   │       (2s)           │   │  Poll   │   │
│   │  Queue  │   │─────────────────────▶│   │  Loop   │   │
│   └─────────┘   │    job ou null       │   └─────────┘   │
│                 │                      │                 │
└─────────────────┘                      └─────────────────┘
```

### Comparaison

| | Event-Driven (SSE) | Polling |
|---|---|---|
| Latence | ~instantané | 0-2s |
| Charge serveur | Faible (1 connexion) | Élevée (requête/2s) |
| Complexité | Mercure requis | Simple |
| Fallback | Poll toutes les 30s | - |

---

## Contrat API

### Authentification

```
POST /api/authentication_token
Headers:
  X-Api-Key: <apiKey>
  X-Api-Secret: <apiSecret>

Response:
{
  "token": "eyJ...",
  "expires_in": 3600,
  "expires_at": 1767972859,
  "type": "printer_agent",
  "agent": {
    "id": "01KEHJ71KY45DBZZAFY67XJBC9",
    "name": "Poste 1",
    "store": "Magasin principal"
  },
  "mercure": {
    "token": "eyJ...",
    "url": "https://example.com/.well-known/mercure",
    "topic": "/printer-agent/01KEHJ71KY45DBZZAFY67XJBC9/jobs"
  }
}
```

> `mercure` est optionnel. Présent uniquement si le serveur supporte Mercure SSE.

### Heartbeat

```
POST /api/printer-agent/ping
Headers:
  Authorization: Bearer <token>
Body: {}

→ 200 OK
```

### Récupérer le prochain job

```
GET /api/printer-agent/jobs/next
Headers:
  Authorization: Bearer <token>

Query params (optionnels):
  - type: receipt | label | sticker_image
  - printer_code: code imprimante spécifique
  - lease_duration: durée lease en secondes (défaut 60)

Response (job disponible):
{
  "success": true,
  "data": {
    "job_id": "01KXXX...",
    "lease_id": "uuid-du-lease",
    "lease_until": "2026-01-09T15:30:00+00:00",
    "type": "receipt",
    "payload": { ... },
    "retry_count": 0,
    "printer": {
      "code": "USB001",
      "name": "EPSON TM-T20",
      "type": "receipt"
    }
  }
}

Response (aucun job):
{
  "success": true,
  "data": null
}
```

### Confirmer un job

```
POST /api/printer-agent/jobs/{jobId}/ack
Headers:
  Authorization: Bearer <token>
Body:
{
  "lease_id": "uuid-du-lease",
  "success": true,
  "error_message": ""
}

→ 200 OK
```

### Synchroniser les imprimantes

```
POST /api/printer-agent/printers
Headers:
  Authorization: Bearer <token>
Body:
{
  "printers": [
    {
      "code": "epson-receipt",
      "name": "EPSON TM-T20III",
      "type": "receipt",
      "description": "/dev/usb/epson_tmt20iii"
    }
  ]
}

Types valides: receipt, label, a4

Response:
{
  "success": true,
  "data": {
    "created": 1,
    "updated": 0,
    "removed": 0,
    "total": 1
  }
}
```

---

## Payloads par type de job

### `receipt` (ticket de caisse)

```json
{
  "barcode": "POS-20260109-001",
  "items": [
    {
      "name": "Livre neuf",
      "quantity": 2,
      "unit_price": "12.50"
    },
    {
      "name": "Carnet A5",
      "quantity": 1,
      "unit_price": "4.90"
    }
  ],
  "payments": ["Carte bancaire", "Espèces"],
  "store_address_1": "Mon Magasin",
  "store_address_2": "123 Rue du Commerce, 75001 Paris",
  "store_phone": "01 23 45 67 89",
  "store_vat": "BE 1234.567.890",
  "store_social": "@monmagasin",
  "store_website": "www.monmagasin.fr"
}
```

| Champ | Type | Requis | Description |
|-------|------|--------|-------------|
| `barcode` | string | Oui | Code-barres imprimé en bas du ticket |
| `items` | array | Oui | Liste des articles |
| `items[].name` | string | Oui | Nom (max 22 caractères affichés) |
| `items[].quantity` | int | Oui | Quantité |
| `items[].unit_price` | string | Oui | Prix unitaire (ex: "12.50") |
| `payments` | string[] | Non | Modes de paiement |
| `store_address_1` | string | Non | Ligne 1 adresse |
| `store_address_2` | string | Non | Ligne 2 adresse |
| `store_phone` | string | Non | Téléphone |
| `store_vat` | string | Non | Numéro TVA |
| `store_social` | string | Non | Réseaux sociaux |
| `store_website` | string | Non | Site web |

> Le total est calculé automatiquement. La date/heure est prise à l'impression.
> Les champs `store_*` peuvent être configurés via variables d'environnement.

### `label` (étiquette prix)

```json
{
  "name": "Livre - Les Misérables",
  "price_text": "19.99 €",
  "barcode": "3760123456789",
  "footer": "Occasion - Bon état"
}
```

| Champ | Type | Requis | Description |
|-------|------|--------|-------------|
| `name` | string | Oui | Nom du produit |
| `price_text` | string | Oui | Prix formaté avec devise |
| `barcode` | string | Oui | Code-barres EAN/ISBN |
| `footer` | string | Non | Texte additionnel |

### `sticker_image` (image)

```json
{
  "image_url": "https://example.com/image.png"
}
```

ou

```json
{
  "image_data": "iVBORw0KGgo..."
}
```

| Champ | Type | Requis | Description |
|-------|------|--------|-------------|
| `image_url` | string | Oui* | URL de l'image |
| `image_data` | string | Oui* | Image en base64 |

*Un des deux requis. `image_data` prioritaire si les deux présents.

Formats: PNG, JPEG, GIF, WebP. Taille max: 10 Mo.

### `open_drawer` (tiroir-caisse)

```json
{}
```

Payload vide.

---

## Mode CLI

Commandes pour tests manuels ou intégration scripts.

### Détecter les imprimantes

```sh
printer detect
```

### Imprimer un ticket test

```sh
printer receipt-test --device /dev/usb/epson_tmt20iii --logo /path/logo.png
```

### Ouvrir le tiroir-caisse

```sh
printer open-drawer --device /dev/usb/epson_tmt20iii
```

### Imprimer une étiquette prix

```sh
printer label "Livre" "12.90 €" 9781234567890 --footer "Chapitre Neuf"
```

### Imprimer un sticker adresse

```sh
printer sticker-address "Chapitre Neuf" "21 Avenue des Combattants" "1370 Jodoigne"
```

### Imprimer une image

```sh
printer sticker-image ./path/to/image.png
```

---

## Variables d'environnement

### Agent

| Variable | Description |
|----------|-------------|
| `PRINT_AGENT_API_URL` | URL de l'API |
| `PRINT_AGENT_API_KEY` | Clé API |
| `PRINT_AGENT_API_SECRET` | Secret API |
| `PRINT_AGENT_POLL_INTERVAL` | Intervalle polling (ex: `2s`) |
| `PRINT_AGENT_PING_INTERVAL` | Intervalle ping (ex: `30s`) |
| `PRINT_AGENT_SYNC_INTERVAL` | Intervalle sync (ex: `10s`) |
| `PRINT_AGENT_HEALTH_ADDR` | Adresse health server |

### Imprimantes

| Variable | Description |
|----------|-------------|
| `RECEIPT_PRINTER_DEVICE` | Device imprimante ticket |
| `RECEIPT_LOGO_PATH` | Logo pour tickets |
| `STORE_ADDRESS_LINE1` | Adresse ligne 1 |
| `STORE_ADDRESS_LINE2` | Adresse ligne 2 |
| `STORE_PHONE` | Téléphone |
| `STORE_VAT_NUMBER` | Numéro TVA |
| `STORE_SOCIAL_HANDLE` | Réseaux sociaux |
| `STORE_WEBSITE` | Site web |
| `PYTHON_PATH` | Chemin Python |
| `LABEL_SCRIPT_PATH` | Script étiquettes |
| `STICKER_SCRIPT_PATH` | Script stickers |

---

## Test sans imprimante

### Tickets et tiroir (ESC/POS vers fichier)

```sh
RECEIPT_PRINTER_DEVICE=/tmp/receipt.bin printer receipt-test
printer open-drawer --device /tmp/drawer.bin
```

### Étiquettes (mock brother_ql)

```sh
export PATH="$(pwd)/scripts:$PATH"
chmod +x scripts/mock_brother_ql.sh
ln -sf mock_brother_ql.sh scripts/brother_ql
printer label "Test" "9.99 €" 1234567890123
```

---

## Health Server

Si `-health-addr` est configuré, l'agent expose :

- `GET /health` - Statut (healthy/degraded)
- `GET /metrics` - Compteurs jobs/imprimantes
- `GET /status` - Statut combiné local + serveur

---
