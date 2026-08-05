#!/usr/bin/env bash
set -euo pipefail
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUNTIME="$PROJECT_ROOT/.runtime"; LOG_DIR="$RUNTIME/logs"; PID_DIR="$RUNTIME/pids"
mkdir -p "$LOG_DIR" "$PID_DIR"
find "$PROJECT_ROOT" -path "$PROJECT_ROOT/.git" -prune -o -type f -name '._*' -delete
[[ -f "$PROJECT_ROOT/.env" ]] || cp "$PROJECT_ROOT/.env.example" "$PROJECT_ROOT/.env"
while IFS='=' read -r key val; do
  [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue
  export "$key=$val"
done < "$PROJECT_ROOT/.env"

port_owner() { lsof -tiTCP:"$1" -sTCP:LISTEN 2>/dev/null | head -1 || true; }
is_project_pid() {
  local pid="$1" cmd cwd
  cmd="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  cwd="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -1)"
  [[ "$cmd" == *"$PROJECT_ROOT"* || "$cwd" == "$PROJECT_ROOT"* || "$cmd" == *"uvicorn app.main:app"* ]]
}
ensure_free() {
  local port="$1" service="$2" pid
  pid="$(port_owner "$port")"; [[ -z "$pid" ]] && return 0
  local cmd; cmd="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  if is_project_pid "$pid"; then kill "$pid"; sleep 1
  else echo "HATA: $port portu başka bir uygulama tarafından kullanılıyor: $cmd"; exit 1; fi
}
wait_url() {
  local url="$1" log="$2"
  for _ in {1..90}; do curl -fsS "$url" >/dev/null 2>&1 && return 0; sleep 1; done
  echo "Servis başlamadı: $url"; tail -100 "$log"; exit 1
}
cleanup() { bash "$PROJECT_ROOT/stop-local.sh"; }
trap cleanup INT TERM EXIT
bash "$PROJECT_ROOT/stop-local.sh"

REDIS_CLI=/opt/homebrew/opt/redis/bin/redis-cli
if ! "$REDIS_CLI" ping 2>/dev/null | grep -q PONG; then
  brew services start redis >/dev/null
  for _ in {1..30}; do "$REDIS_CLI" ping 2>/dev/null | grep -q PONG && break; sleep 1; done
fi

PYTHON_BIN="$(command -v python3.12 || true)"; [[ -n "$PYTHON_BIN" ]] || { echo "HATA: Python 3.12 bulunamadı."; exit 1; }
VENV="$HOME/.venvs/sosyalmedyatakip-scraper"
[[ -x "$VENV/bin/python" ]] || "$PYTHON_BIN" -m venv "$VENV"
"$VENV/bin/pip" install --disable-pip-version-check -q -r "$PROJECT_ROOT/scraper/requirements.txt"
"$VENV/bin/python" -m playwright install chromium
"$VENV/bin/python" -c "from playwright.async_api import async_playwright; print('playwright-ok')"

ensure_free 8091 scraper
(cd "$PROJECT_ROOT/scraper" && exec "$VENV/bin/python" -m uvicorn app.main:app --host 127.0.0.1 --port 8091) >"$LOG_DIR/scraper.log" 2>&1 & echo $! >"$PID_DIR/scraper.pid"
wait_url http://127.0.0.1:8091/health "$LOG_DIR/scraper.log"

ensure_free 8080 backend
(cd "$PROJECT_ROOT/backend" && go mod download && exec env BACKEND_PORT=8080 REDIS_URL=redis://127.0.0.1:6379/0 REDIS_HOST=127.0.0.1 REDIS_PORT=6379 go run ./cmd/api) >"$LOG_DIR/backend.log" 2>&1 & echo $! >"$PID_DIR/backend.pid"
wait_url http://127.0.0.1:8080/health "$LOG_DIR/backend.log"

ensure_free 5173 frontend
(cd "$PROJECT_ROOT/frontend" && npm install && exec npm run dev -- --host 127.0.0.1 --port 5173) >"$LOG_DIR/frontend.log" 2>&1 & echo $! >"$PID_DIR/frontend.pid"
wait_url http://127.0.0.1:5173 "$LOG_DIR/frontend.log"
open http://127.0.0.1:5173
echo "Frontend: http://127.0.0.1:5173"
echo "Backend:  http://127.0.0.1:8080"
echo "Scraper:  http://127.0.0.1:8091"
echo "Redis:    127.0.0.1:6379"
echo "Durdurmak için Ctrl+C kullanın."
wait
