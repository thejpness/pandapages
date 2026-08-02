#!/usr/bin/env bash

# Wait for the final PostgreSQL server rather than the entrypoint temporary server.
# Callers use a one-second Docker health interval, so two healthy observations
# are deliberately separated by at least one interval.
wait_for_stable_postgres() {
  local container=$1
  local postgres_user=$2
  local target_database=$3
  local description=$4
  local status
  local running
  local health
  local consecutive_healthy=0
  local attempt

  for ((attempt = 0; attempt < 60; attempt++)); do
    status=$(docker inspect --format '{{.State.Running}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$container") || {
      printf '%s container disappeared before stable readiness\n' "$description" >&2
      return 1
    }
    IFS='|' read -r running health <<<"$status"

    [[ "$running" == true ]] || {
      printf '%s stopped before stable readiness\n' "$description" >&2
      return 1
    }
    [[ "$health" != unhealthy ]] || {
      printf '%s became unhealthy\n' "$description" >&2
      return 1
    }

    if [[ "$health" == healthy ]]; then
      consecutive_healthy=$((consecutive_healthy + 1))
      if ((consecutive_healthy >= 2)); then
        if docker exec "$container" \
          psql -X --username="$postgres_user" --dbname="$target_database" \
            --set=ON_ERROR_STOP=1 --tuples-only --command='SELECT 1;' \
            >/dev/null 2>&1; then
          return 0
        fi
        # A failed probe can coincide with the entrypoint restart window. Start
        # a new stable-health observation sequence within the bounded timeout.
        consecutive_healthy=0
      fi
    else
      consecutive_healthy=0
    fi

    sleep 1
  done

  printf '%s did not reach stable SQL readiness\n' "$description" >&2
  return 1
}
