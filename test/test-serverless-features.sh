#!/usr/bin/env bash
# Manual end-to-end smoke test of `verda container` and `verda batchjob`
# against a real API. Not run by CI: it creates and deletes billable
# deployments, and several steps are interactive.
#
#   make build
#   ./test/test_serverless_features.sh              # pause after every step
#   PAUSE=0 ./test/test_serverless_features.sh      # run straight through
#   INTERACTIVE=1 ./test/test_serverless_features.sh # also drive wizards/confirm prompts
#   KEEP_HOME=1 ./test/test_serverless_features.sh  # keep the throwaway VERDA_HOME
#
# VERDA_HOME is always a fresh temp dir (your real ~/.verda is never touched)
# and is removed on exit unless KEEP_HOME=1 — it holds the credentials you
# enter at `auth login`. Set VERDA_HOME yourself to reuse a dir across runs.
#
# Steps that are expected to fail (validation, missing --yes) are marked
# "expect:"; a non-zero exit there is the correct outcome.

set -u

cd "$(dirname "$0")/.."
V=${V:-./bin/verda}
PAUSE=${PAUSE:-1}
INTERACTIVE=${INTERACTIVE:-0}
KEEP_HOME=${KEEP_HOME:-0}

if [[ ! -x "$V" ]]; then
  echo "binary $V not found; run 'make build' first" >&2
  exit 1
fi

if [[ -z "${VERDA_HOME:-}" ]]; then
  VERDA_HOME=$(mktemp -d)
  export VERDA_HOME
  if [[ "$KEEP_HOME" != "1" ]]; then
    trap 'rm -rf "$VERDA_HOME"' EXIT
  fi
fi
echo "VERDA_HOME=$VERDA_HOME"

CT=cli-test-nginx
JOB=cli-test-job
SECRET=cli-test-secret
FILES=cli-test-files
IMG=nginx:1.27.3
IMG2=nginx:1.27.4
COMPUTE=${COMPUTE:-RTX PRO 6000}

step() { printf '\n\033[1;34m### %s\033[0m\n' "$*"; }

# run: echo the command, run it, report the exit code, optionally pause.
run() {
  printf '\033[1;32m$ %s\033[0m\n' "$*"
  "$@"
  local rc=$?
  if [[ $rc -ne 0 ]]; then printf '\033[1;31m(exit %d)\033[0m\n' "$rc"; fi
  if [[ "$PAUSE" == "1" ]]; then read -r -p "[enter to continue] "; fi
  return 0
}

# interactive: like run, but only when INTERACTIVE=1 (prompts, wizards).
interactive() {
  if [[ "$INTERACTIVE" != "1" ]]; then
    printf '\033[2m(skipped, INTERACTIVE=0) $ %s\033[0m\n' "$*"
    return 0
  fi
  run "$@"
}

step "0. Setup and login"
run $V version
run $V auth login
run $V auth show

step "1. Catalogs and prerequisites"
run $V container registry-creds list
run $V container secret list
run $V container file-secret list
run $V container secret create --name $SECRET --value s3cr3t-value
run $V container secret list
run $V container secret list --agent -o json
echo "hello-from-file" > "$VERDA_HOME/hello.txt"
run $V container file-secret create --name $FILES --file hello.txt="$VERDA_HOME/hello.txt"
run $V container file-secret list

step "2. Container: create (flag-driven)"
run $V container create \
  --name $CT \
  --image $IMG \
  --compute "$COMPUTE" --compute-size 1 \
  --port 80 --healthcheck-path / \
  --env APP_ENV=test \
  --env-secret APP_TOKEN=$SECRET \
  --secret-mount $FILES:/run/secrets \
  --min-replicas 0 --max-replicas 2 \
  --concurrency 4 --queue-preset balanced \
  --yes --debug

step "3. Container: read paths"
run $V container list
run $V container list --status healthy
run $V container ls -o json
run $V container describe $CT
run $V container describe $CT -o yaml
run $V container describe $CT --agent -o json
run $V container replicas $CT
run $V container replicas $CT -o json
run $V container env list $CT

step "4. Container: env vars"
run $V container env set $CT --env LOG_LEVEL=debug --env MODEL_NAME=none
run $V container env set $CT --env-secret API_KEY=$SECRET
run $V container env list $CT
run $V container env unset --deployment $CT LOG_LEVEL --yes
run $V container env unset --deployment $CT MODEL_NAME --agent -o json --yes
run $V container env list $CT -o json

step "5. Container: scale"
run $V container scale $CT --min-replicas 1 --max-replicas 3 --yes --wait --debug
run $V container scale $CT --cpu-util 70 --yes --wait
run $V container scale $CT --cpu-util off --concurrency 8 --yes --wait
run $V container scale $CT --queue-preset cost-saver --yes --wait
run $V container scale $CT --queue-load 2.5 --scale-up-delay 10s --scale-down-delay 2m --request-ttl 10m --yes --wait
run $V container scale $CT --min-replicas 0 --yes --wait
run $V container describe $CT

step "5b. Container: scale — expect: client-side rejections"
run $V container scale $CT --max-replicas 0 --yes
run $V container scale $CT --min-replicas 5 --max-replicas 2 --yes
run $V container scale $CT --cpu-util 3 --yes
run $V container scale $CT --agent -o json --min-replicas 1   # expect: --yes required in agent mode
run $V container scale $CT --min-replicas 1 --yes --agent -o json --wait
interactive $V container scale

step "6. Container: update"
run $V container update $CT --image $IMG2 --yes --debug
run $V container update $CT --env APP_ENV=prod --env EXTRA=1 --yes
run $V container env list $CT
run $V container update $CT --spot --max-replicas 4 --yes
run $V container update $CT --no-spot --yes
run $V container update $CT --healthcheck-off --yes
run $V container update $CT --healthcheck-path / --healthcheck-port 80 --yes
run $V container update $CT --cmd nginx --cmd -g --cmd "daemon off;" --yes
run $V container update $CT --agent -o json --image $IMG   # expect: --yes required
run $V container update $CT --agent -o json --image $IMG --yes
run $V container describe $CT

step "7. Container: lifecycle actions"
run $V container pause $CT
run $V container list --status paused
run $V container resume $CT
run $V container describe $CT
interactive $V container restart $CT
run $V container restart $CT --yes
run $V container restart $CT --agent -o json   # expect: --yes required
run $V container restart $CT --agent -o json --yes
run $V container purge-queue $CT --yes
run $V container purge-queue $CT --agent -o json --yes
run $V container pause $CT --agent -o json
run $V container resume $CT --agent -o json

step "8. Container: interactive create wizard (name it cli-test-wizard)"
interactive $V container create
interactive $V container list
interactive $V container delete cli-test-wizard

step "9. Batch jobs"
run $V batchjob create \
  --name $JOB \
  --image $IMG \
  --compute "$COMPUTE" --compute-size 1 \
  --port 80 --deadline 10m \
  --env JOB_ENV=test \
  --env-secret JOB_TOKEN=$SECRET \
  --secret-mount $FILES:/run/secrets \
  --max-replicas 2 --request-ttl 5m \
  --yes --debug
run $V batchjob list
run $V batchjob ls -o json
run $V batchjob describe $JOB
run $V batchjob describe $JOB -o yaml
run $V batchjob describe $JOB --agent -o json
run $V batchjob update $JOB --deadline 30m --max-replicas 3 --yes --debug
run $V batchjob update $JOB --image $IMG2 --env JOB_ENV=prod --yes
run $V batchjob update $JOB --agent -o json --max-replicas 1   # expect: --yes required
run $V batchjob update $JOB --agent -o json --max-replicas 1 --yes
run $V batchjob describe $JOB
run $V batchjob pause $JOB
run $V batchjob list
run $V batchjob resume $JOB
interactive $V batchjob purge-queue $JOB
run $V batchjob purge-queue $JOB --yes
run $V batchjob purge-queue $JOB --agent -o json --yes
run $V batchjob pause $JOB --agent -o json
run $V batchjob resume $JOB --agent -o json

step "9b. Batch jobs — expect: validation errors"
run $V batchjob create --name x --image $IMG --compute "$COMPUTE" --yes                 # expect: --deadline required
run $V batchjob create --name x --image nginx:latest --compute "$COMPUTE" --deadline 1m --yes   # expect: :latest rejected
interactive $V batchjob create
interactive $V batchjob delete cli-test-job-wizard

step "10. Error contracts (nonexistent deployment)"
run $V container describe does-not-exist
run $V container describe does-not-exist --agent -o json
run $V batchjob describe does-not-exist --agent -o json
run $V container delete does-not-exist --agent -o json --yes

step "11. Cleanup (deployments first; in-use secrets need --force)"
interactive $V batchjob delete $JOB
run $V batchjob delete $JOB --yes
run $V batchjob list
run $V container delete $CT --agent -o json   # expect: --yes required
run $V container delete $CT --timeout-ms 60000 --yes --debug
run $V container list
run $V container file-secret delete $FILES --yes
run $V container secret delete $SECRET --yes
run $V container secret list
run $V container registry-creds list

step "12. Shell completion"
run $V __complete container ""
run $V __complete batchjob ""
run $V __complete container scale --queue-preset ""

step "Done"
if [[ "$KEEP_HOME" == "1" ]]; then
  echo "kept VERDA_HOME=$VERDA_HOME (contains your credentials; remove it when finished)"
fi
