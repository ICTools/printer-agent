# print-agent

Agent d'impression pour points de vente. Fonctionne en mode autonome (polling API) ou en CLI.

## Installation

### Pré-requis système

- Linux avec accès aux devices imprimante (`/dev/usb/*`, `/dev/lp*`)
- Python 3
- `brother_ql` CLI (pour étiquettes Brother QL)
- Pillow (Python imaging)

### Dépendances Python

```sh
python3 -m pip install pillow brother_ql
```

### Permissions device

```sh
# Vérifier les permissions
ls -l /dev/usb/lp0

# Ajouter l'utilisateur au groupe lp si nécessaire
sudo usermod -aG lp $USER
```

## Build

```sh
go build -o print-agent ./cmd/print-agent
```

---

## Mode Agent (recommandé)

L'agent se connecte à une API distante, récupère les jobs d'impression et les exécute automatiquement.

### Démarrage

```sh
./print-agent run \
  -api-url https://api.monsite.com \
  -api-key $API_KEY \
  -api-secret $API_SECRET
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
./print-agent run \
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
./print-agent detect
```

### Imprimer un ticket test

```sh
./print-agent receipt-test --device /dev/usb/epson_tmt20iii --logo /path/logo.png
```

### Ouvrir le tiroir-caisse

```sh
./print-agent open-drawer --device /dev/usb/epson_tmt20iii
```

### Imprimer une étiquette prix

```sh
./print-agent label "Livre" "12.90 €" 9781234567890 --footer "Chapitre Neuf"
```

### Imprimer un sticker adresse

```sh
./print-agent sticker-address "Chapitre Neuf" "21 Avenue des Combattants" "1370 Jodoigne"
```

### Imprimer une image

```sh
./print-agent sticker-image ./path/to/image.png
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
RECEIPT_PRINTER_DEVICE=/tmp/receipt.bin ./print-agent receipt-test
./print-agent open-drawer --device /tmp/drawer.bin
```

### Étiquettes (mock brother_ql)

```sh
export PATH="$(pwd)/scripts:$PATH"
chmod +x scripts/mock_brother_ql.sh
ln -sf mock_brother_ql.sh scripts/brother_ql
./print-agent label "Test" "9.99 €" 1234567890123
```

---

## Health Server

Si `-health-addr` est configuré, l'agent expose :

- `GET /health` - Statut (healthy/degraded)
- `GET /metrics` - Compteurs jobs/imprimantes
- `GET /status` - Statut combiné local + serveur

---

## Troubleshooting

- **brother_ql not found** : vérifier installation et PATH
- **Permission denied** : vérifier groupe `lp` et règles udev
- **Texte illisible** : les accents sont convertis en ASCII
- **TLS error en local** : utiliser `-insecure`
