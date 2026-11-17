# verda network -- Manage private networks (beta)

Commands, flags and behavior: `verda network --help` and `verda network <command> --help`. Commands take IDs, not names.

## Workflow

```bash
NETWORK_ID=$(verda network create --name prod -o json | jq -r .id)
SUBNET_ID=$(verda network subnet create --network "$NETWORK_ID" -l FIN-03 -o json | jq -r .id)
verda network subnet list --network "$NETWORK_ID"
verda network subnet delete "$SUBNET_ID" --network "$NETWORK_ID"
verda network delete "$NETWORK_ID"
```

## Agent Mode

- Missing `--name`, `--network`, `--location` or an ID: `MISSING_REQUIRED_FLAGS` (exit 2).
- Bad input (mode, name, CIDR, location): `VALIDATION_ERROR` (exit 2), before the create request.
- Delete without `--yes`: `CONFIRMATION_REQUIRED` (exit 2).
- Unknown ID: `NOT_FOUND` (exit 5).
- Delete success: `{"resource", "id", "name", "action": "delete", "status": "completed"}`.
