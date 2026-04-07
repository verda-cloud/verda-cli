# Agent Error Format

This document defines the structured error format used when Verda CLI runs in `--agent` mode. All errors are written to **stderr** as JSON so that calling agents can parse them programmatically.

## Envelope

Every error follows this JSON envelope:

```json
{
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable explanation",
    "details": {}
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `code` | string | Machine-readable error category (UPPER_SNAKE_CASE). Agents use this to decide what to do next. |
| `message` | string | Human-readable explanation. Agents can show this to the user or use it for context. |
| `details` | object | Optional structured data specific to the error code. Contents vary by code. |

## Error Codes

### `MISSING_REQUIRED_FLAGS`

**Exit code:** 2

Required CLI flags were not provided. The agent should read `details.missing` and either provide the missing values or ask the user.

```json
{
  "error": {
    "code": "MISSING_REQUIRED_FLAGS",
    "message": "required flags not provided",
    "details": {
      "missing": ["--instance-type", "--os", "--hostname"]
    }
  }
}
```

**Agent action:** Read the missing flags, determine values, and retry the command with all required flags.

### `CONFIRMATION_REQUIRED`

**Exit code:** 2

A destructive action (delete, shutdown) was attempted without `--yes`. The agent should confirm with the user, then retry with `--yes`.

```json
{
  "error": {
    "code": "CONFIRMATION_REQUIRED",
    "message": "destructive action \"delete\" requires --yes in agent mode",
    "details": {
      "action": "delete"
    }
  }
}
```

**Agent action:** Ask the user for confirmation. If confirmed, retry with `--yes`.

### `INTERACTIVE_PROMPT_BLOCKED`

**Exit code:** 2

A command tried to open an interactive prompt (select, text input, etc.) in agent mode. The `details` include the prompt text and available choices so the agent can determine the correct flag to pass.

```json
{
  "error": {
    "code": "INTERACTIVE_PROMPT_BLOCKED",
    "message": "select prompt not available in agent mode",
    "details": {
      "prompt_type": "select",
      "prompt": "Select instance (type to filter)",
      "choices": ["gpu-runner  ● running  1V100.6V  FIN-01  203.0.113.10", "Cancel"]
    }
  }
}
```

**Agent action:** Determine which flag provides the needed input and retry with that flag.

### `AUTH_ERROR`

**Exit code:** 3

Authentication failed. Credentials are missing, invalid, or expired.

```json
{
  "error": {
    "code": "AUTH_ERROR",
    "message": "no credentials configured\n\nRun \"verda auth login\" to set up your credentials",
    "details": {
      "status": 401
    }
  }
}
```

**Agent action:** Tell the user to run `verda auth login`.

### `API_ERROR`

**Exit code:** 4

The Verda Cloud API returned an error not covered by a more specific code.

```json
{
  "error": {
    "code": "API_ERROR",
    "message": "API error 500: internal server error",
    "details": {
      "status": 500
    }
  }
}
```

**Agent action:** Show the error to the user. May be transient (retry) or permanent.

### `NOT_FOUND`

**Exit code:** 5

The requested resource does not exist.

```json
{
  "error": {
    "code": "NOT_FOUND",
    "message": "not found",
    "details": {
      "status": 404
    }
  }
}
```

**Agent action:** Verify the resource ID is correct. List resources to find the right one.

### `INSUFFICIENT_BALANCE`

**Exit code:** 6

The account does not have enough balance for the requested operation.

```json
{
  "error": {
    "code": "INSUFFICIENT_BALANCE",
    "message": "insufficient balance",
    "details": {
      "status": 402
    }
  }
}
```

**Agent action:** Show balance to user (`verda cost balance`), suggest adding funds.

### `VALIDATION_ERROR`

**Exit code:** 2

A flag value failed validation.

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "invalid value for hostname: too long",
    "details": {
      "field": "hostname",
      "reason": "too long"
    }
  }
}
```

**Agent action:** Fix the invalid value and retry.

### `ERROR`

**Exit code:** 1

Catch-all for errors that don't match a more specific code.

```json
{
  "error": {
    "code": "ERROR",
    "message": "unexpected: connection refused",
    "details": null
  }
}
```

**Agent action:** Show the error message to the user.

## Exit Codes

| Code | Meaning | Agent Should |
|------|---------|-------------|
| 0 | Success | Process stdout JSON |
| 1 | General error | Show error to user |
| 2 | Bad input (missing flags, validation, needs confirmation) | Fix input and retry |
| 3 | Authentication error | Tell user to run `verda auth login` |
| 4 | API error (server-side) | Show error, possibly retry |
| 5 | Resource not found | Verify resource ID |
| 6 | Insufficient balance | Show balance, suggest adding funds |

## Classification Logic

Errors are classified in this priority order:

1. **Already an AgentError** (from explicit checks in commands) -- returned as-is
2. **SDK `APIError`** -- mapped by HTTP status code (401/403 -> AUTH_ERROR, 404 -> NOT_FOUND, 402 -> INSUFFICIENT_BALANCE, others -> API_ERROR)
3. **SDK `ValidationError`** -- mapped to VALIDATION_ERROR with field and reason
4. **Auth-related message heuristic** -- messages containing "no credentials configured", "unauthorized", "token expired" -> AUTH_ERROR
5. **Fallback** -- generic ERROR with the original message

## For Developers

### Adding a new error code

1. Add the constant to `agent_error.go` (exit code + constructor)
2. Add the code to this document with example JSON and agent action
3. Add classification logic in `ClassifyError` if needed
4. Add a test in `agent_error_test.go`

### When to return AgentError directly vs rely on ClassifyError

- **Return AgentError directly** when the command has specific context (e.g., which flags are missing, which action needs confirmation). This gives agents the richest information.
- **Rely on ClassifyError** for errors bubbling up from the SDK or other libraries. The classifier handles the common cases automatically.

### Implementation

- Error types: `internal/verda-cli/cmd/util/agent_error.go`
- Classification: `ClassifyError()` in the same file
- Entry point: `cmd/verda/main.go` calls `ClassifyError()` on all errors
