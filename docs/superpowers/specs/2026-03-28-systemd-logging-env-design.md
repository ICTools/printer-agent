# Design: Service systemd, logs journald et fichier .env

**Date**: 2026-03-28

## Contexte

L'agent print-agent tourne actuellement en avant-plan uniquement. Les logs sortent sur stdout/stderr sans persistance. Il n'y a aucun chargement de fichier `.env` — la configuration passe par flags CLI ou `os.Getenv()`.

## Objectifs

1. Lancer l'agent automatiquement au démarrage de la machine en tant que service de fond
2. Persister les logs avec rotation automatique
3. Charger proprement un fichier `.env` avec support des espaces dans les valeurs

## Décisions

| Point | Choix | Justification |
|-------|-------|---------------|
| Type de service | systemd user-level (`systemctl --user`) | L'agent tourne sous l'utilisateur de session existant, pas besoin de root |
| Logs | journald (via stdout/stderr) | Zéro modification du code Go, rotation native |
| Fichier .env | `EnvironmentFile=` dans le service systemd | Pas de dépendance Go supplémentaire, gestion correcte des espaces |

## 1. Service systemd user-level

### Fichier `print-agent.service`

Emplacement d'installation : `~/.config/systemd/user/print-agent.service`

Contenu :
- `Type=simple` — l'agent est un processus long
- `EnvironmentFile=` — pointe vers le fichier d'environnement
- `ExecStart=` — chemin absolu vers le binaire avec les arguments nécessaires
- `Restart=on-failure` — redémarrage automatique en cas de crash
- `RestartSec=5` — délai avant redémarrage
- `WantedBy=default.target` — démarrage automatique à l'ouverture de session

### Démarrage sans session graphique

Pour que le service démarre même sans login interactif (ex: serveur headless, reboot sans écran) :
```bash
loginctl enable-linger <username>
```

### Commandes de gestion

```bash
systemctl --user enable print-agent    # activer au démarrage
systemctl --user start print-agent     # démarrer maintenant
systemctl --user stop print-agent      # arrêter
systemctl --user restart print-agent   # redémarrer
systemctl --user status print-agent    # voir le statut
```

## 2. Logs via journald

Aucune modification du code Go. Le package standard `log` écrit sur stderr, qui est capturé automatiquement par systemd/journald.

### Consultation

```bash
journalctl --user -u print-agent -f          # suivre en temps réel
journalctl --user -u print-agent --since today  # logs du jour
journalctl --user -u print-agent -p err      # erreurs uniquement
```

### Rotation

Gérée nativement par journald via `/etc/systemd/journald.conf` :
- `SystemMaxUse=` / `RuntimeMaxUse=` — taille max
- `MaxRetentionSec=` — durée max de rétention

Pas de configuration spécifique requise — les défauts journald suffisent.

## 3. Fichier d'environnement

### Emplacement

`/home/<user>/.config/print-agent/env`

### Format (compatible `EnvironmentFile=`)

```bash
# Configuration print-agent
PRINT_AGENT_API_URL=https://example.com
PRINT_AGENT_API_KEY=my-api-key
PRINT_AGENT_API_SECRET=my secret with spaces
PRINT_AGENT_POLL_INTERVAL=2s
PRINT_AGENT_PING_INTERVAL=30s
PRINT_AGENT_SYNC_INTERVAL=10s
PRINT_AGENT_HEALTH_ADDR=:8080
```

Règles du format systemd `EnvironmentFile=` :
- Pas de `export` en préfixe
- Les guillemets doubles sont optionnels mais recommandés si la valeur contient des espaces : `KEY="value with spaces"`
- Les lignes commençant par `#` sont des commentaires
- Les lignes vides sont ignorées

### Fichier `.env.example`

Fournir un `.env.example` à la racine du projet avec toutes les variables documentées, servant de template.

## Livrables

1. **`deploy/print-agent.service`** — fichier service systemd (versionné dans le repo)
2. **`.env.example`** — template des variables d'environnement
3. **`deploy/README.md`** — instructions d'installation et de gestion du service (enable-linger, commandes systemctl, consultation des logs)

## Hors scope

- Pas de modification du code Go pour le logging (journald capture stdout/stderr)
- Pas de bibliothèque godotenv (systemd gère le .env)
- Pas de Dockerfile ou autre orchestrateur
