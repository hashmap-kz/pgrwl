#!/bin/bash

APP_NAME="pg_receivewal"
APP_PATH="pg_receivewal" # compiled binary
PID_FILE="/tmp/${APP_NAME}.pid"
LOG_FILE="/tmp/${APP_NAME}.log"
WAL_PATH="/tmp/wal-archive"

mkdir -p "${WAL_PATH}"
chown -R postgres:postgres "${WAL_PATH}"

# CLI args
ARGS=(
  "-D" "${WAL_PATH}"
  "-S" "pg_receivewal"
  "--no-loop"
  "--verbose"
  "--no-password"
  "--synchronous"
  "--dbname" "dbname=replication options=-cdatestyle=iso replication=true application_name=pg_receivewal"
)

# Environment variables
export PGHOST="localhost"
export PGPORT="5432"
export PGUSER="postgres"
export PGPASSWORD="postgres"

# Prepare slot
pg_receivewal \
  --no-password \
  --slot=pg_receivewal \
  --create-slot \
  --if-not-exists

start() {
  if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    echo "$APP_NAME is already running (PID $(cat "$PID_FILE"))"
    exit 1
  fi

  echo "Starting $APP_NAME..."
  nohup "$APP_PATH" "${ARGS[@]}" >>"$LOG_FILE" 2>&1 &
  echo $! >"$PID_FILE"
  echo "$APP_NAME started (PID $!)"
}

stop() {
  if [ ! -f "$PID_FILE" ]; then
    echo "$APP_NAME is not running"
    exit 1
  fi

  PID=$(cat "$PID_FILE")
  echo "Stopping $APP_NAME (PID $PID)..."
  kill "$PID" && rm -f "$PID_FILE"
  echo "$APP_NAME stopped"
}

status() {
  if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    echo "$APP_NAME is running (PID $(cat "$PID_FILE"))"
  else
    echo "$APP_NAME is not running"
  fi
}

case "$1" in
start) start ;;
stop) stop ;;
status) status ;;
restart)
  stop
  sleep 1
  start
  ;;
*)
  echo "Usage: $0 {start|stop|restart|status}"
  exit 1
  ;;
esac
