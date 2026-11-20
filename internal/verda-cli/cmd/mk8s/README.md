# verda mk8s -- Managed Kubernetes (experimental)

> Experimental: the managed Kubernetes API may change in incompatible ways. Commands, flags and output may change without a deprecation period.

`verda mk8s` (aliases `k8s`, `kubernetes`) manages clusters, their node pools (`pool`, `np`: the worker VMs) and their kubeconfigs (client certificates for `kubectl`), plus `locations`. Commands, flags and defaults: `verda mk8s <command> --help`.

## Workflow

mk8s never creates the private network or the subnet. Create them first with `verda network`, in the cluster's location, and pass their IDs.

```bash
verda mk8s locations
NETWORK_ID=$(verda network create --name k8s -o json | jq -r .id)
SUBNET_ID=$(verda network subnet create --network "$NETWORK_ID" -l FIN-03 -o json | jq -r .id)

verda mk8s cluster create --name prod -l FIN-03 --kubernetes-version 1.37.0 \
  --network "$NETWORK_ID" --subnet "$SUBNET_ID" --wait
verda mk8s node-pool create --cluster prod --name workers --instance-type 1V100.6V \
  --ssh-key "$KEY_ID" --replicas 2 --wait
verda mk8s kubeconfig create --cluster prod --common-name alice --admin

export KUBECONFIG=~/.kube/verda-prod.alice.yaml
kubectl get nodes

verda mk8s node-pool scale workers --cluster prod --replicas 0 --yes
verda mk8s cluster delete prod --yes        # node pools, worker VMs and kubeconfigs go with it
```

Cluster and node pool names are unique in the project (pool names too, not only in their cluster), so `--cluster prod` is enough: the CLI finds the location with one list call; `-l` skips that call. Kubeconfigs are named by an ID that the API chooses.

## Prompts and `--yes`

| Command | On a terminal | In scripts, agent mode, `-o json\|yaml` |
|---|---|---|
| `cluster create`, `node-pool create`, `kubeconfig create` | Asks for missing values, shows a summary, asks to confirm | Required flags must be given; no `--yes` needed |
| `node-pool scale` up | Shows a summary, asks to confirm | No `--yes` needed |
| `node-pool scale` down (to 0 too), all deletes, `cluster upgrade` | Red warning, asks to confirm (`--yes` skips it) | `--yes` required, else `CONFIRMATION_REQUIRED` |
| Any command without its cluster, node pool or kubeconfig | Picker | `--cluster` and the positional target are required |

## Waiting

Without `--wait` a command reports `accepted`; with `--wait` (`kubeconfig create` always waits) it polls and reports `completed`.

- A timeout exits non-zero; the change keeps going in the background. Ctrl+C stops only the wait.
- After a node pool timeout the backend keeps creating workers, which bill even before they join: the error says how to stop (delete the pool, or scale back with `--yes`).
- `node-pool scale` to the current count sends nothing (status `unchanged`); with `--wait` it still waits for a pool that is not yet settled.
- Deletes and `cluster upgrade` have no `--wait`: the VMs of a deleted cluster or pool are deleted in the background, and the API reports the requested version, not the running one.

## Kubeconfigs

The kubeconfig YAML holds a client private key.

- It is written only to a file with mode 0600 (default `~/.kube/verda-<cluster>.<common-name>.yaml`, `~/.kube` created 0700), or to stdout with `--file -`; never to `-o json|yaml`, `describe` or `--debug`. The file is checked before a certificate is issued; a symlink or a directory is always refused.
- Certificates cannot be revoked before they expire (90 days unless `--duration`): `kubeconfig delete` does not disable downloaded copies; deleting the cluster does. `kubeconfig create --force` leaves the earlier certificate valid on the server (the CLI says so).
- If `kubeconfig create` stops before the kubeconfig is ready, or cannot write the file, the error gives the ID and the `kubeconfig save` command to run. Running `create` again issues another certificate.

## When a create fails

A create is sent once and never retried. After a 5xx, a 408, a timeout or a network error the object may exist anyway: the CLI says so (agent mode: `error.details.warning`). Check `list` before you run `create` again.

## Agent mode

- `MISSING_REQUIRED_FLAGS` (exit 2) for a missing flag or target (`kubeconfig create` lists `--admin`/`--group` in `details.one_of`); `CONFIRMATION_REQUIRED` (exit 2) without `--yes`; `VALIDATION_ERROR` (exit 2) for bad values and unusable kubeconfig files.
- Unknown cluster: `NOT_FOUND` (exit 5); other API errors keep the API message (`API_ERROR`, exit 4). Not enabled for the project: `AUTH_ERROR` (exit 3).
- Results: creates `{"status", "cluster"|"pool"}`, scale adds `previous_replicas`, upgrade `previous_version`; deletes `{"resource", "name", "location", "action", "status"}`; kubeconfig `create`/`save` print the metadata with `file`, never the YAML. A failed `--wait` still writes the result, with `wait_error`, and exits non-zero.
- A failed `kubeconfig create` wait or write adds `kubeconfig_id`, `save_command` and `warning` to `error.details`.
