# TODO: Mode Agent (Polling API)

## Architecture proposée

```
┌─────────────────────────────────────────────────────────────────┐
│                         print-agent                             │
│                                                                 │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────────┐  │
│  │   CLI        │    │   Agent      │    │   Dispatcher     │  │
│  │   (existant) │    │   (nouveau)  │    │   (nouveau)      │  │
│  └──────────────┘    └──────┬───────┘    └────────┬─────────┘  │
│                             │                     │             │
│                             ▼                     ▼             │
│                      ┌──────────────┐    ┌──────────────────┐  │
│                      │  API Client  │    │  Printer Registry │  │
│                      │  (nouveau)   │    │  (nouveau)        │  │
│                      └──────┬───────┘    └────────┬─────────┘  │
│                             │                     │             │
└─────────────────────────────┼─────────────────────┼─────────────┘
                              │                     │
                              ▼                     ▼
                       ┌─────────────┐    ┌─────────────────────┐
                       │  API        │    │  Imprimantes        │
                       │  distante   │    │  /dev/usb/...       │
                       └─────────────┘    └─────────────────────┘
```

### Composants

1. **API Client** (`internal/api/client.go`)
   - Client HTTP pour communiquer avec l'API distante
   - `FetchJobs()` - Récupère les jobs en attente
   - `AckJob(id)` - Confirme qu'un job est traité
   - `FailJob(id, error)` - Signale un échec

2. **Job Model** (`internal/api/job.go`)
   - Structure représentant un job d'impression
   - Types: `receipt`, `label`, `sticker-image`, `open-drawer`

3. **Printer Registry** (`internal/registry/registry.go`)
   - Gère la liste des imprimantes disponibles
   - Associe un identifiant logique à un device physique
   - Détection automatique au démarrage

4. **Dispatcher** (`internal/dispatcher/dispatcher.go`)
   - Route les jobs vers le bon driver selon le type
   - Gère la concurrence (1 job par imprimante à la fois)

5. **Agent** (`internal/agent/agent.go`)
   - Boucle principale de polling
   - Gestion du cycle de vie (start/stop/graceful shutdown)
   - Configuration (intervalle de polling, retry policy)

6. **Commande `run`** (`cmd/print-agent/main.go`)
   - Nouvelle commande CLI pour lancer l'agent
   - Flags: `--api-url`, `--poll-interval`, `--config`

---

## Tâches

### Phase 1: Modèles et Client API ✅

- [x] **1.1** Créer `internal/api/job.go` - Définir la structure `Job`
  - Champs: ID, Type, PrinterID, Payload, CreatedAt
  - Types de jobs: receipt, label, sticker-image, open-drawer
  - Payload flexible (JSON selon le type)

- [x] **1.2** Créer `internal/api/client.go` - Client HTTP
  - Configuration: BaseURL, Timeout, Auth (API key ou Bearer token)
  - `FetchJobs(ctx) ([]Job, error)` - GET /jobs?status=pending
  - `AckJob(ctx, jobID) error` - POST /jobs/{id}/ack
  - `FailJob(ctx, jobID, reason) error` - POST /jobs/{id}/fail
  - Gestion des erreurs HTTP et retry basique

- [x] **1.3** Ajouter des tests unitaires pour le client API
  - Mock server HTTP pour tester les différents scénarios
  - Tests: succès, erreur réseau, erreur 4xx/5xx, timeout

### Phase 2: Registry et Dispatcher ✅

- [x] **2.1** Créer `internal/registry/registry.go` - Registre d'imprimantes
  - Structure `PrinterInfo`: ID, Type (receipt/label), DevicePath, Available
  - `Detect() []PrinterInfo` - Détecte les imprimantes branchées
  - `Get(id) (*PrinterInfo, error)` - Récupère une imprimante par ID
  - Support configuration statique (fichier) + détection dynamique

- [x] **2.2** Créer `internal/dispatcher/dispatcher.go` - Dispatcher
  - `Dispatch(job Job, printer PrinterInfo) error`
  - Switch sur job.Type pour appeler le bon driver
  - Conversion du payload JSON vers les structures existantes

- [x] **2.3** Ajouter mutex par imprimante
  - Éviter les impressions concurrentes sur le même device
  - Map de mutex par PrinterID

### Phase 3: Agent et boucle de polling ✅

- [x] **3.1** Créer `internal/agent/agent.go` - Agent principal
  - Structure `Agent` avec config, client, registry, dispatcher
  - `Start(ctx)` - Démarre la boucle de polling
  - `Stop()` - Arrêt graceful (termine le job en cours)

- [x] **3.2** Implémenter la boucle de polling
  - Intervalle configurable (défaut: 5s)
  - Backoff exponentiel en cas d'erreur API
  - Logging des actions (fetch, dispatch, ack, fail)

- [x] **3.3** Gestion des signaux système
  - SIGINT/SIGTERM → arrêt graceful
  - SIGHUP → reload config (optionnel)

### Phase 4: Intégration CLI ✅

- [x] **4.1** Ajouter commande `run` dans main.go
  - Flags: `--api-url`, `--api-key`, `--poll-interval`, `--timeout`, `--verbose`
  - Validation des paramètres requis
  - Affichage du statut au démarrage
  - Support variables d'environnement

- [x] **4.2** Support fichier de configuration (optionnel)
  - Variables d'environnement: PRINT_AGENT_API_URL, PRINT_AGENT_API_KEY, PRINT_AGENT_POLL_INTERVAL
  - (fichier YAML/JSON: non implémenté, variables env suffisantes)

- [x] **4.3** Mode verbose/debug
  - Flag `--verbose` pour logs détaillés
  - Affichage des jobs reçus et du résultat

### Phase 5: Robustesse ✅

- [x] **5.1** Retry policy pour les impressions
  - Nombre max de tentatives configurable (défaut: 3)
  - Délai entre tentatives avec backoff exponentiel
  - Erreurs non-retriables (parsing, job type inconnu)

- [x] **5.2** Health check endpoint
  - Mini serveur HTTP local pour monitoring (`-health-addr :8080`)
  - `/health` → statut de l'agent (healthy/degraded)
  - `/metrics` → compteurs jobs traités/échoués, imprimantes

- [x] **5.3** Tests d'intégration
  - Test end-to-end avec mock API et mock imprimante
  - Scénarios: job succès, graceful shutdown, reconnexion API, health degraded

---

## Authentification JWT ✅

L'agent utilise une authentification JWT avec les endpoints suivants:

### Obtention du token
```
POST /api/authentication_token
Headers:
  X-Api-Key: <apiKey>
  X-Api-Secret: <apiSecret>

Response 200:
{
  "token": "eyJ...",
  "expires_in": 3600,
  "expires_at": 1767972859,
  "type": "printer_agent",
  "agent": {
    "id": "01KEHJ71KY45DBZZAFY67XJBC9",
    "name": "Post 1",
    "store": "Magasin principal"
  }
}
```

### Ping (heartbeat)
```
POST /api/printer-agent/ping
Headers:
  Authorization: Bearer <token>
Body: {}

→ 200 OK (toutes les 5 secondes)
```

---

## Contrat API (endpoints métier)

### Workflow complet de l'agent
1. `POST /api/authentication_token` → obtenir JWT
2. `POST /api/printer-agent/ping` → toutes les ~30s
3. `GET /api/printer-agent/jobs/next` → polling toutes les 1-2s
4. `POST /api/printer-agent/jobs/{jobId}/ack` → confirmer impression

### GET /api/printer-agent/jobs/next
```
Headers:
  Authorization: Bearer <token>

Paramètres optionnels (query string):
  - type: Filtrer par type de job (receipt, label, sticker_image)
  - printer_code: Filtrer par imprimante spécifique
  - lease_duration: Durée du lease en secondes (défaut 60, min 10, max 300)

Réponse si job disponible:
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

Réponse si aucun job:
{
  "success": true,
  "data": null
}
```

### POST /api/printer-agent/jobs/{jobId}/ack
```
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

---

## Variables d'environnement

| Variable | Description |
|----------|-------------|
| `PRINT_AGENT_API_URL` | URL de base de l'API (ex: `http://localhost`) |
| `PRINT_AGENT_API_KEY` | Clé API pour l'authentification |
| `PRINT_AGENT_API_SECRET` | Secret API pour l'authentification |
| `PRINT_AGENT_POLL_INTERVAL` | Intervalle de polling (ex: `5s`) |
| `PRINT_AGENT_PING_INTERVAL` | Intervalle de ping (ex: `5s`) |
| `PRINT_AGENT_SYNC_INTERVAL` | Intervalle de sync imprimantes (ex: `10s`) |
| `PRINT_AGENT_HEALTH_ADDR` | Adresse du serveur health (ex: `:8080`) |

---

## Synchronisation des imprimantes ✅

L'agent détecte automatiquement les imprimantes connectées/déconnectées et les synchronise avec le serveur:

### Endpoint
```
POST /api/printer-agent/printers
Headers:
  Authorization: Bearer <token>
Body:
{
  "printers": [
    {
      "code": "epson-receipt",
      "name": "epson-receipt",
      "type": "receipt",
      "description": "/dev/usb/epson_tmt20iii"
    }
  ]
}

Types valides: receipt, label, a4

Response 200:
{
  "success": true,
  "data": {
    "created": 1,
    "updated": 1,
    "removed": 0,
    "total": 2
  }
}
```

### Comportement
- Sync au démarrage uniquement si des imprimantes sont détectées
- Détection périodique des changements (intervalle configurable, défaut: 10s)
- Log des connexions/déconnexions d'imprimantes
- Sync automatique **uniquement** quand un changement est détecté
- Affichage des statistiques de sync (created, updated, removed, total)

---

## Endpoints supplémentaires ✅

### GET /api/printer-agent/printers
Liste les imprimantes enregistrées côté serveur pour cet agent.
```json
{
  "success": true,
  "data": [
    {
      "id": "01JGX...",
      "code": "USB001",
      "name": "EPSON TM-T20III",
      "type": "receipt",
      "typeLabel": "Ticket",
      "description": "Imprimante tickets USB",
      "isActive": true
    }
  ]
}
```

### GET /api/printer-agent/status
Vérifie le statut de l'agent côté serveur (self-check).
```json
{
  "success": true,
  "data": {
    "id": "01JGX...",
    "name": "Poste Caisse 1",
    "code": "AGENT-A1B2C3D4",
    "isActive": true,
    "isOnline": true,
    "lastPingAt": "2026-01-09T10:30:00+00:00",
    "store": {
      "id": "01JGX...",
      "name": "Magasin Paris"
    },
    "printersCount": 2
  }
}
```

### Health Server - Nouvel endpoint /status
Le serveur de health local expose maintenant `/status` qui combine:
- Statut local (healthy/degraded, imprimantes, jobs)
- Statut serveur (en appelant GET /api/printer-agent/status)
