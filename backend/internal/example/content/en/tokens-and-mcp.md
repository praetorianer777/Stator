---
labels: guide, connections
---
# Tokens and the MCP tools

%%properties%%

| Audience | Everybody |
|---|---|
| Reading time | 3 minutes |

%%end%%

## Personal access tokens

**Access tokens** in the account menu makes tokens for scripts and assistants. A token acts as you, in one organization, and never does more than you may.

- **Read only** keeps it to reading.
- **Spaces** limits it to the spaces you pick; it then reaches nothing of the organization as a whole.
- It runs out after 7, 30, 90 or 365 days, or never.
- Its secret is shown once, then never again: copy it then. **Revoke** stops it at once. Administrators of the organization see and revoke every token.

A script sends the token in each request, in the header `Authorization: Bearer` followed by the token.

## Connecting an assistant

Stator speaks the Model Context Protocol at `/api/v1/mcp`. **Connect an assistant** on the tokens page shows the address and the settings to give a client:

```json
{
  "mcpServers": {
    "stator": {
      "type": "http",
      "url": "https://stator.example.com/api/v1/mcp",
      "headers": { "Authorization": "Bearer YOUR-TOKEN" }
    }
  }
}
```

The assistant then reads and writes as you, with tools such as `search`, `get_page`, `get_space_outline`, `create_page`, `update_page`, `add_comment`, `list_my_tasks`, `create_post` and `create_calendar_event`. A read only token offers only the reading tools. Deleting, permissions, publishing, moving and sharing are left to people.
