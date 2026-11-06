# deploy/

| Fichier | Rôle |
|---|---|
| `install.sh` | installation en une commande (`curl -fsSL https://raw.githubusercontent.com/ICTools/printer-agent/main/deploy/install.sh \| bash`), ou `--from-source` depuis un clone |
| `print-agent.service` | service systemd utilisateur de l'agent (`printer run`) |
| `printer-update.service`, `printer-update.timer` | mise à jour automatique quotidienne (désactivée par défaut, `printer autoupdate on`) |

Les trois unités sont **embarquées dans le binaire** (`assets.go`) et écrites dans `~/.config/systemd/user/` par
`printer setup` : modifier ces fichiers ici, pas sur le PC.

Installation, commandes, mise à jour, migration et dépannage : voir le [README](../README.md).
