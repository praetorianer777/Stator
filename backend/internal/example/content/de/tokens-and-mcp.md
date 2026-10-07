---
labels: anleitung, anbindungen
---
# Tokens und die MCP-Werkzeuge

%%properties%%

| Zielgruppe | Alle |
|---|---|
| Lesezeit | 3 Minuten |

%%end%%

## Persönliche Zugriffstokens

**Zugriffstokens** im Kontomenü erstellt Tokens für Skripte und Assistenten. Ein Token handelt als Sie, in einer Organisation, und tut nie mehr, als Sie dürfen.

- **Nur lesen** beschränkt es aufs Lesen.
- **Bereiche** beschränkt es auf die gewählten Bereiche; dann erreicht es nichts von der Organisation als Ganzer.
- Es läuft nach 7, 30, 90 oder 365 Tagen ab, oder nie.
- Sein Geheimnis wird einmal gezeigt, dann nie wieder: Kopieren Sie es dann. **Widerrufen** stoppt es sofort. Administratoren der Organisation sehen und widerrufen jedes Token.

Ein Skript schickt das Token mit jeder Anfrage, im Header `Authorization: Bearer` gefolgt vom Token.

## Einen Assistenten verbinden

Stator spricht das Model Context Protocol unter `/api/v1/mcp`. **Einen Assistenten verbinden** auf der Token-Seite zeigt die Adresse und die Einstellungen für einen Client:

```json
{
  "mcpServers": {
    "stator": {
      "type": "http",
      "url": "https://stator.example.com/api/v1/mcp",
      "headers": { "Authorization": "Bearer IHR-TOKEN" }
    }
  }
}
```

Der Assistent liest und schreibt dann als Sie, mit Werkzeugen wie `search`, `get_page`, `get_space_outline`, `create_page`, `update_page`, `add_comment`, `list_my_tasks`, `create_post` und `create_calendar_event`. Ein Token, das nur lesen darf, bietet nur die lesenden Werkzeuge. Löschen, Berechtigungen, Veröffentlichen, Verschieben und Teilen bleiben Menschen überlassen.
