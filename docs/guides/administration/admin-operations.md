# Admin Operations

This is a brief guide for admins and moderators managing content on the registry. All actions should be taken in line with the [moderation guidelines](moderation-guidelines.md).

## Prerequisites

- Admin account with @modelcontextprotocol.io email
  - If you are a maintainer and would like an account, ask in the Discord
- `gcloud` CLI installed and configured
- `curl` and `jq` installed

## Authentication

```bash
# Run this, then run the export command it outputs
./tools/admin/auth.sh
```

## Edit a Server

Step 1: Download Server

```bash
export SERVER_ID="<server-uuid>"
curl -s "https://registry.modelcontextprotocol.io/v0/servers/${SERVER_ID}" > server.json
```

Step 2: Open `server.json` and make changes. You cannot change the server name.

Step 3: Push Changes

```bash
curl -X PUT "https://registry.modelcontextprotocol.io/v0/servers/${SERVER_ID}" \
  -H "Authorization: Bearer ${REGISTRY_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"server\": $(cat server.json)}"
```

## Takedown a Server

```bash
export SERVER_ID="<server-uuid>"
./tools/admin/takedown.sh
```

This soft deletes the server. If you need to delete the content of a server (usually only where legally necessary), use the edit workflow above to scrub it all.

## Rate Limiting Configuration

The registry enforces daily publish rate limits to prevent abuse:

### Environment Variables

- `MCP_REGISTRY_RATE_LIMIT_ENABLED`: Enable/disable rate limiting (default: true)
- `MCP_REGISTRY_RATE_LIMIT_PER_DAY`: Maximum publishes per user per day (default: 10)
- `MCP_REGISTRY_RATE_LIMIT_EXEMPTIONS`: Comma-separated list of exempt users or patterns

### Exemption Patterns

Exemptions support wildcard patterns:
- Exact match: `anthropic` (exempts user "anthropic")
- Wildcard: `anthropic/*` (exempts "anthropic", "anthropic.claude", etc.)
- Multiple exemptions: `anthropic/*,modelcontextprotocol,github/*`

### Notes

- Rate limits are per authenticated user (not per namespace)
- Users with global admin permissions automatically bypass rate limits
- Limits reset on a rolling 24-hour window
- The counter is stored in the `publish_attempts` database table
