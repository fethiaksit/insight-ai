#!/usr/bin/env bash
set -euo pipefail
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PID_DIR="$PROJECT_ROOT/.runtime/pids"
for service in frontend backend scraper; do
  file="$PID_DIR/$service.pid"
  [[ -f "$file" ]] || continue
  pid="$(tr -cd '0-9' < "$file")"
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    command_line="$(ps -p "$pid" -o command= 2>/dev/null || true)"
    process_cwd="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -1)"
    if [[ "$command_line" == *"$PROJECT_ROOT"* || "$process_cwd" == "$PROJECT_ROOT"* || "$command_line" == *"uvicorn app.main:app"* ]]; then
      kill "$pid" 2>/dev/null || true
    else
      echo "$service PID $pid bu projeye ait görünmediği için durdurulmadı."
    fi
  fi
  rm -f "$file"
done
