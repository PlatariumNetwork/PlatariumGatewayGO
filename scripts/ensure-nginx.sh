#!/usr/bin/env bash
# Ensure Hestia nginx proxies this RPC domain → Gateway REST + /ws.
# Host-specific values: .env.nginx (gitignored). See .env.nginx.example.
# Called before `go build` via scripts/build.sh / update-and-restart.sh.
#
# Do not add location / via *_custom (duplicate with Hestia vhost).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TPL_SRC_DIR="$ROOT_DIR/deploy/nginx/hestia"
PROXY_BLOCK="$(mktemp)"
RENDERED_TPL="$(mktemp)"
RENDERED_STPL="$(mktemp)"

log() { echo "[nginx-gateway] $*"; }
warn() { echo "[nginx-gateway] WARN: $*" >&2; }

cleanup() { rm -f "$PROXY_BLOCK" "$RENDERED_TPL" "$RENDERED_STPL"; }
trap cleanup EXIT

# Load .env.nginx without overriding variables already set in the environment.
load_env_file() {
  local f="$1" line key val
  [[ -f "$f" ]] || return 0
  log "loading $f"
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%"${line##*[![:space:]]}"}"
    line="${line#"${line%%[![:space:]]*}"}"
    [[ -z "$line" || "$line" == \#* ]] && continue
    [[ "$line" == export\ * ]] && line="${line#export }"
    key="${line%%=*}"
    val="${line#*=}"
    key="${key%"${key##*[![:space:]]}"}"
    key="${key#"${key%%[![:space:]]*}"}"
    [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue
    if [[ -n "${!key+x}" ]]; then
      continue
    fi
    # strip optional surrounding quotes
    if [[ "$val" =~ ^\".*\"$ ]]; then val="${val:1:${#val}-2}"; fi
    if [[ "$val" =~ ^\'.*\'$ ]]; then val="${val:1:${#val}-2}"; fi
    export "$key=$val"
  done <"$f"
}

load_env_file "$ROOT_DIR/.env.nginx"
# Optional overlap with gateway deploy env (ports only if unset)
load_env_file "$ROOT_DIR/.env.gateway"

HESTIA_USER="${NGINX_HESTIA_USER:-admin}"
REST_PORT="${NGINX_REST_PORT:-${GATEWAY01_REST_PORT:-1812}}"
WS_PORT="${NGINX_WS_PORT:-${GATEWAY01_WS_PORT:-1813}}"
TPL_NAME="${NGINX_TPL_NAME:-platarium-gateway}"
CORS_ORIGIN="${NGINX_CORS_ORIGIN:-}"
MARKER_BEGIN="# BEGIN PLATARIUM_GATEWAY_PROXY"
MARKER_END="# END PLATARIUM_GATEWAY_PROXY"

# Infer domain from Hestia layout .../web/<domain>/public_html — no hardcoded hostnames.
detect_domain() {
  if [[ -n "${NGINX_DOMAIN:-}" ]]; then
    echo "$NGINX_DOMAIN"
    return
  fi
  local cwd
  cwd="$(pwd -P 2>/dev/null || pwd)"
  if [[ "$cwd" =~ /web/([^/]+)/public_html(/|$) ]]; then
    echo "${BASH_REMATCH[1]}"
    return
  fi
  if [[ -n "${NGINX_DOMAIN_DEFAULT:-}" ]]; then
    echo "$NGINX_DOMAIN_DEFAULT"
    return
  fi
  warn "NGINX_DOMAIN not set and cwd is not .../web/<domain>/public_html"
  warn "set NGINX_DOMAIN in .env.nginx (see .env.nginx.example)"
  echo ""
}

DOMAIN="$(detect_domain)"

build_cors_block() {
  local origin="$1"
  # First origin only (browser allows a single Access-Control-Allow-Origin value).
  local primary="*"
  if [[ -n "$origin" ]]; then
    primary="${origin%%,*}"
    primary="${primary%"${primary##*[![:space:]]}"}"
    primary="${primary#"${primary%%[![:space:]]*}"}"
  fi
  cat <<EOF
    # Single CORS value at the edge (hide upstream * from Go to avoid duplicate headers)
    add_header Access-Control-Allow-Origin "${primary}" always;
    add_header Access-Control-Allow-Methods "GET, POST, PUT, DELETE, OPTIONS" always;
    add_header Access-Control-Allow-Headers "Content-Type, Authorization, X-Requested-With" always;
    add_header Access-Control-Max-Age "86400" always;
    add_header Vary "Origin" always;

    if (\$request_method = OPTIONS) {
        return 204;
    }
EOF
}

# Directives inside each proxy location — strip Go's CORS so nginx's add_header is alone.
cors_proxy_hide() {
  cat <<'EOF'
        proxy_hide_header Access-Control-Allow-Origin;
        proxy_hide_header Access-Control-Allow-Methods;
        proxy_hide_header Access-Control-Allow-Headers;
        proxy_hide_header Access-Control-Max-Age;
        proxy_hide_header Access-Control-Expose-Headers;
EOF
}

CORS_BLOCK="$(build_cors_block "$CORS_ORIGIN")"
CORS_HIDE="$(cors_proxy_hide)"

fill_placeholders() {
  local src="$1" dest="$2"
  REST_PORT="$REST_PORT" WS_PORT="$WS_PORT" CORS_BLOCK="$CORS_BLOCK" CORS_HIDE="$CORS_HIDE" \
  awk '
    BEGIN {
      cors = ENVIRON["CORS_BLOCK"]
      hide = ENVIRON["CORS_HIDE"]
      rest = ENVIRON["REST_PORT"]
      ws = ENVIRON["WS_PORT"]
    }
    {
      line = $0
      gsub(/__REST_PORT__/, rest, line)
      gsub(/__WS_PORT__/, ws, line)
      if (index(line, "__CORS_BLOCK__") > 0) { print cors; next }
      if (index(line, "__CORS_HIDE__") > 0) { print hide; next }
      print line
    }
  ' "$src" >"$dest"
}

fill_placeholders "$TPL_SRC_DIR/${TPL_NAME}.tpl" "$RENDERED_TPL" 2>/dev/null || true
fill_placeholders "$TPL_SRC_DIR/${TPL_NAME}.stpl" "$RENDERED_STPL" 2>/dev/null || true

cat >"$PROXY_BLOCK" <<EOF
    ${MARKER_BEGIN}
$(build_cors_block "$CORS_ORIGIN")

    location /ws {
$(cors_proxy_hide)
        proxy_pass http://127.0.0.1:${WS_PORT}/;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 7d;
        proxy_send_timeout 7d;
        proxy_buffering off;
        proxy_cache off;
    }

    location / {
$(cors_proxy_hide)
        proxy_pass http://127.0.0.1:${REST_PORT};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 600s;
        proxy_send_timeout 600s;
        proxy_buffering off;
    }
    ${MARKER_END}
EOF

run_priv() {
  if "$@" 2>/dev/null; then return 0; fi
  if command -v sudo >/dev/null 2>&1; then sudo -n "$@"; else return 1; fi
}

write_bytes() {
  local dest="$1" src="$2"
  if cp "$src" "$dest" 2>/dev/null; then
    chmod 644 "$dest" 2>/dev/null || true
    return 0
  fi
  if command -v sudo >/dev/null 2>&1; then
    sudo -n mkdir -p "$(dirname "$dest")" 2>/dev/null || true
    sudo -n cp "$src" "$dest" && sudo -n chmod 644 "$dest"
    return $?
  fi
  return 1
}

conf_is_ok() {
  local f="$1"
  [[ -f "$f" ]] || return 1
  grep -qF "$MARKER_BEGIN" "$f" || return 1
  grep -q "proxy_pass http://127.0.0.1:${REST_PORT}" "$f" || return 1
  grep -qE 'location[[:space:]]+/ws[[:space:]]*\{' "$f" || return 1
  grep -q "proxy_pass http://127.0.0.1:${WS_PORT}" "$f" || return 1
  return 0
}

log "domain=${DOMAIN:-"(unset)"} REST=:${REST_PORT} WS=:${WS_PORT} cors=${CORS_ORIGIN:-"(none)"}"

if [[ "${1:-}" == "--discover" || "${NGINX_DISCOVER:-}" == "1" ]]; then
  log "—— nginx discovery ——"
  command -v nginx >/dev/null && log "nginx: $(command -v nginx)" || log "nginx: (not found)"
  if [[ -n "$DOMAIN" ]]; then
    ls -la /home/"${HESTIA_USER}"/conf/web/"${DOMAIN}".nginx* 2>/dev/null || log "flat customs: (none)"
    ls -la "/home/${HESTIA_USER}/conf/web/${DOMAIN}/" 2>/dev/null || log "domain dir: (missing)"
  fi
  ls -la /usr/local/hestia/data/templates/web/nginx/"${TPL_NAME}".* 2>/dev/null || log "templates: (none)"
  if command -v nginx >/dev/null 2>&1 && [[ -n "$DOMAIN" ]]; then
    nginx -T 2>/dev/null | grep -nE "configuration file |server_name .*${DOMAIN}|proxy_pass|location /ws" | head -80 || true
  fi
  log "—— end discovery ——"
  exit 0
fi

if [[ "${NGINX_SKIP:-}" == "1" ]]; then
  log "NGINX_SKIP=1 — skipping"
  exit 0
fi

if ! command -v nginx >/dev/null 2>&1; then
  warn "nginx not on PATH — skip (local build)"
  exit 0
fi

if [[ -z "$DOMAIN" ]]; then
  warn "no domain — skip nginx ensure (set NGINX_DOMAIN in .env.nginx)"
  exit 0
fi

remove_conflicting_confd() {
  local p
  for p in /etc/nginx/conf.d/"${TPL_NAME}".conf /etc/nginx/conf.d/platarium-gateway.conf; do
    [[ -f "$p" ]] || continue
    local bak="${p}.disabled.$(date +%s)"
    if mv "$p" "$bak" 2>/dev/null || run_priv mv "$p" "$bak"; then
      log "moved conflicting conf.d → $bak"
    fi
  done
}

strip_duplicate_location_includes() {
  local base="/home/${HESTIA_USER}/conf/web"
  local f
  for f in \
    "${base}/${DOMAIN}.nginx.conf_custom" \
    "${base}/${DOMAIN}.nginx.ssl.conf_custom" \
    "${base}/${DOMAIN}/nginx.conf_${TPL_NAME//-/_}" \
    "${base}/${DOMAIN}/nginx.ssl.conf_${TPL_NAME//-/_}"
  do
    [[ -f "$f" ]] || continue
    if grep -qE 'location[[:space:]]+/[[:space:]]*\{|location[[:space:]]+/ws|PLATARIUM_GATEWAY_PROXY' "$f" 2>/dev/null; then
      local bak="${f}.bak.dupfix.$(date +%s)"
      cp "$f" "$bak" 2>/dev/null || true
      if printf '# cleared by ensure-nginx (do not put location / or /ws here)\n' >"$f" 2>/dev/null \
        || run_priv bash -c "printf '%s\n' '# cleared by ensure-nginx' > '$f'"; then
        log "cleared duplicate proxy locations from $f"
      fi
    fi
  done
}

install_hestia_templates() {
  local dest_dirs=()
  [[ -d /usr/local/hestia/data/templates/web/nginx ]] && dest_dirs+=("/usr/local/hestia/data/templates/web/nginx")
  [[ -d /usr/local/hestia/data/templates/web/nginx/php-fpm ]] && dest_dirs+=("/usr/local/hestia/data/templates/web/nginx/php-fpm")
  [[ ${#dest_dirs[@]} -eq 0 ]] && { warn "Hestia template dirs missing"; return 1; }

  if [[ ! -s "$RENDERED_TPL" || ! -s "$RENDERED_STPL" ]]; then
    fill_placeholders "$TPL_SRC_DIR/${TPL_NAME}.tpl" "$RENDERED_TPL"
    fill_placeholders "$TPL_SRC_DIR/${TPL_NAME}.stpl" "$RENDERED_STPL"
  fi

  local d
  for d in "${dest_dirs[@]}"; do
    write_bytes "$d/${TPL_NAME}.tpl" "$RENDERED_TPL" && log "installed $d/${TPL_NAME}.tpl" || return 1
    write_bytes "$d/${TPL_NAME}.stpl" "$RENDERED_STPL" && log "installed $d/${TPL_NAME}.stpl" || return 1
    if [[ -f "$TPL_SRC_DIR/${TPL_NAME}.sh" ]]; then
      write_bytes "$d/${TPL_NAME}.sh" "$TPL_SRC_DIR/${TPL_NAME}.sh" || true
      chmod 755 "$d/${TPL_NAME}.sh" 2>/dev/null || run_priv chmod 755 "$d/${TPL_NAME}.sh" || true
    fi
  done
  return 0
}

apply_hestia_template() {
  local ok=0
  if command -v v-change-web-domain-proxy-tpl >/dev/null 2>&1; then
    if run_priv v-change-web-domain-proxy-tpl "$HESTIA_USER" "$DOMAIN" "$TPL_NAME" yes; then
      log "applied proxy template via v-change-web-domain-proxy-tpl"; ok=1
    fi
  fi
  if [[ "$ok" -eq 0 ]] && command -v v-change-web-domain-tpl >/dev/null 2>&1; then
    if run_priv v-change-web-domain-tpl "$HESTIA_USER" "$DOMAIN" "$TPL_NAME" yes; then
      log "applied web template via v-change-web-domain-tpl"; ok=1
    fi
  fi
  if [[ "$ok" -eq 0 ]] && command -v v-rebuild-web-domain >/dev/null 2>&1; then
    run_priv v-rebuild-web-domain "$HESTIA_USER" "$DOMAIN" yes && log "rebuilt web domain" && ok=1 || true
  fi
  [[ "$ok" -eq 1 ]]
}

patch_location_slash() {
  local conf="$1"
  [[ -f "$conf" ]] || return 1
  if conf_is_ok "$conf"; then
    log "OK — already proxied: $conf"
    return 1
  fi
  local tmp out
  tmp="$(mktemp)"
  out="$(mktemp)"
  cp "$conf" "$tmp"
  python3 - "$tmp" "$PROXY_BLOCK" "$out" <<'PY' || return 1
import re
import sys

path, block_path, out_path = sys.argv[1], sys.argv[2], sys.argv[3]
text = open(path, encoding="utf-8", errors="replace").read()
block = open(block_path, encoding="utf-8", errors="replace").read().rstrip() + "\n"

begin = "# BEGIN PLATARIUM_GATEWAY_PROXY"
end = "# END PLATARIUM_GATEWAY_PROXY"
if begin in text and end in text:
    i = text.index(begin)
    j = text.index(end) + len(end)
    while j < len(text) and text[j] == "\n":
        j += 1
    open(out_path, "w", encoding="utf-8").write(text[:i] + block + text[j:])
    sys.exit(0)

lines = text.splitlines(keepends=True)
cleaned = []
i = 0
while i < len(lines):
    if re.match(r"^\s*location\s+/ws/?\s*\{", lines[i]):
        depth = 0
        while i < len(lines):
            for ch in lines[i]:
                if ch == "{":
                    depth += 1
                elif ch == "}":
                    depth -= 1
            i += 1
            if depth == 0:
                break
        continue
    cleaned.append(lines[i])
    i += 1
lines = cleaned

start = None
for idx, line in enumerate(lines):
    if re.match(r"^\s*location\s+/\s*\{", line):
        start = idx
        break

if start is None:
    sys.exit(2)

depth = 0
end_idx = None
for idx in range(start, len(lines)):
    for ch in lines[idx]:
        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                end_idx = idx
                break
    if end_idx is not None:
        break

if end_idx is None:
    sys.exit(3)

indent = re.match(r"^(\s*)", lines[start]).group(1)
block_lines = []
for bl in block.splitlines():
    if bl.strip() == "":
        block_lines.append("\n")
    else:
        block_lines.append(indent + bl.lstrip() + "\n")

open(out_path, "w", encoding="utf-8").write("".join(lines[:start] + block_lines + lines[end_idx + 1 :]))
sys.exit(0)
PY
  local rc=$?
  if [[ "$rc" -ne 0 ]]; then
    rm -f "$tmp" "$out"
    return 1
  fi
  cp "$conf" "${conf}.bak.gateway.$(date +%Y%m%d%H%M%S)" 2>/dev/null || true
  if cat "$out" >"$conf" 2>/dev/null || run_priv bash -c "cat '$out' > '$conf'"; then
    log "patched location / + /ws in $conf"
    rm -f "$tmp" "$out"
    return 0
  fi
  warn "cannot write patched $conf"
  rm -f "$tmp" "$out"
  return 1
}

find_vhost_confs() {
  local candidates=(
    "/home/${HESTIA_USER}/conf/web/${DOMAIN}/nginx.ssl.conf"
    "/home/${HESTIA_USER}/conf/web/${DOMAIN}/nginx.conf"
    "/home/${HESTIA_USER}/conf/web/${DOMAIN}.nginx.ssl.conf"
    "/home/${HESTIA_USER}/conf/web/${DOMAIN}.nginx.conf"
    "/etc/nginx/conf.d/domains/${DOMAIN}.ssl.conf"
    "/etc/nginx/conf.d/domains/${DOMAIN}.conf"
  )
  local f
  for f in "${candidates[@]}"; do
    [[ -f "$f" ]] && echo "$f"
  done
}

reload_nginx() {
  local test_out
  test_out="$(nginx -t 2>&1)" || test_out="$(run_priv nginx -t 2>&1)" || {
    warn "nginx -t failed — not reloading"
    echo "$test_out" >&2
    return 1
  }
  log "nginx -t OK"
  if [[ "${NGINX_RELOAD:-1}" == "0" ]]; then return 0; fi
  if command -v systemctl >/dev/null 2>&1 && run_priv systemctl reload nginx; then
    log "nginx reloaded (systemctl)"; return 0
  fi
  if command -v v-restart-service >/dev/null 2>&1 && run_priv v-restart-service nginx; then
    log "nginx reloaded (v-restart-service)"; return 0
  fi
  if run_priv nginx -s reload; then
    log "nginx reloaded (nginx -s reload)"; return 0
  fi
  warn "reload failed"
  return 1
}

remove_conflicting_confd
strip_duplicate_location_includes

changed=0
if [[ -d /usr/local/hestia/data/templates/web/nginx ]] || [[ -d /usr/local/hestia ]]; then
  if install_hestia_templates; then
    if apply_hestia_template; then
      changed=1
    else
      warn "could not auto-apply Hestia template — patching live vhost confs"
    fi
  fi
fi

while IFS= read -r conf; do
  [[ -z "$conf" ]] && continue
  if patch_location_slash "$conf"; then changed=1; fi
done < <(find_vhost_confs)

reload_nginx || true

if command -v curl >/dev/null 2>&1; then
  code="$(curl -sS -o /dev/null -w "%{http_code}" --max-time 8 "https://${DOMAIN}/network" 2>/dev/null || echo 000)"
  log "post-check GET https://${DOMAIN}/network → HTTP ${code}"
  if [[ "$code" == "403" || "$code" == "000" ]]; then
    warn "unexpected HTTP ${code}. Check nginx -t, listeners :${REST_PORT}/:${WS_PORT}, template=${TPL_NAME}"
  elif [[ "$code" == "502" || "$code" == "504" ]]; then
    warn "nginx OK but upstream down — start gateway on :${REST_PORT}/:${WS_PORT}"
  fi
fi

exit 0
