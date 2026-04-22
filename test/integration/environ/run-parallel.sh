#!/usr/bin/env bash
set -euo pipefail

### TODO: should be fixed, currently the same bucket is used for all
### TODO: tests for simplicity, so - there's no way to make a parallel
### TODO: execution without refactoring the whole suite (need proper infra isolation)

COMPOSE_FILE="docker-compose-par.yml"
MAX_PARALLEL=4
LOG_DIR="test_logs/$(date +%Y%m%d_%H%M%S)"
mkdir -p "$LOG_DIR"

TESTS=(
  pg_001_fundamental
  pg_002_write_loop
  pg_003_s3
  pg_004_sftp
  pg_005_restore_localfs
  pg_006_restore_s3
  pg_007_tablespaces_localfs
  pg_008_dynstor
  pg_009_s3_remote_only_restore
  pg_010_timing_parity_1
  pg_010_timing_parity_2
  pg_011_basic_flow
  pg_012_restore_s3_toxiproxy
  pg_014_manager_rest_api
)

echo "Starting infrastructure..."
docker compose -f "$COMPOSE_FILE" up -d minio sshd toxiproxy
docker compose -f "$COMPOSE_FILE" run --rm createbuckets
echo "Infrastructure ready."

echo "Running tests (max $MAX_PARALLEL parallel)..."

# Run via GNU parallel — captures exit codes reliably in results file
parallel \
  --jobs "$MAX_PARALLEL" \
  --joblog "$LOG_DIR/joblog.txt" \
  --results "$LOG_DIR/results" \
  "docker compose -f $COMPOSE_FILE run --rm --no-deps {}" \
  ::: "${TESTS[@]}"

# parallel returns non-zero if any job failed, but we want our own summary
true  # reset $?

echo ""
echo "===== TEST RESULTS ====="
FAILED=0

for test in "${TESTS[@]}"; do
  # joblog columns: Seq, Host, Starttime, JobRuntime, Send, Receive, Exitval, Signal, Command
  code=$(awk -v cmd="$test" '$NF ~ cmd {print $7}' "$LOG_DIR/joblog.txt" | tail -1)
  code=${code:-999}

  if [[ "$code" -eq 0 ]]; then
    echo "  V PASS  $test"
  else
    echo "  X FAIL  $test (exit code: $code)"
    FAILED=1
  fi
done
echo "========================"

if [[ $FAILED -eq 1 ]]; then
  echo ""
  echo "=== FAILED TEST LOGS ==="
  for test in "${TESTS[@]}"; do
    code=$(awk -v cmd="$test" '$NF ~ cmd {print $7}' "$LOG_DIR/joblog.txt" | tail -1)
    if [[ "${code:-1}" -ne 0 ]]; then
      echo "--- $test ---"
      # parallel --results writes stdout/stderr per job
      cat "$LOG_DIR/results/1/$test/stdout" 2>/dev/null | tail -20
      cat "$LOG_DIR/results/1/$test/stderr" 2>/dev/null | tail -20
      echo ""
    fi
  done
fi

docker compose -f "$COMPOSE_FILE" down -v
exit $FAILED