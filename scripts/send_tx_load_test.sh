#!/usr/bin/env bash
# Відправка транзакцій 1→2 та 2→1 зі статистикою (друге вікно консолі).
# Перед запуском у першому вікні має працювати run_100nodes.sh (або хоча б одна testnet-нода на 2812/2813).
#
# Usage:
#   cd PlatariumGatewayGO && ./scripts/send_tx_load_test.sh [ROUNDS]
#   ROUNDS - кількість пар транзакцій (1→2, 2→1); за замовчуванням 20.

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$GATEWAY_DIR/.." && pwd)"
CORE_DIR="$REPO_ROOT/PlatariumCore"

ROUNDS="${1:-20}"
# Скільки нод у мережі - TX відправляються по черзі на різні ноди (round-robin), щоб перевірити розповсюдження
NODES="${SEND_TX_NODES:-100}"
BASE_PORT="${BASE_PORT:-2812}"
BASE_URL="${BASE_URL:-http://localhost:$BASE_PORT}"   # використовується лише для "Waiting for gateway" (нода 0)
SAMPLE_NODES="${SAMPLE_NODES:-0,10,50,99}"   # індекси нод для перевірки балансів (порт = BASE_PORT+10*i)

export PLATARIUM_CLI_PATH="${PLATARIUM_CLI_PATH:-$CORE_DIR/target/release/platarium-cli}"
if [ ! -x "$PLATARIUM_CLI_PATH" ]; then
  echo "ERROR: platarium-cli not found. Build Core: cd $CORE_DIR && cargo build --release"
  exit 1
fi
CLI="$PLATARIUM_CLI_PATH"

# Парсинг виводу generate-mnemonic: Mnemonic: ... Alphanumeric: ...
parse_mnemonic_output() {
  local out=$1
  local mnemonic="" alphanumeric=""
  while IFS= read -r line; do
    line=$(echo "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    if [[ "$line" == Mnemonic:\ * ]]; then mnemonic="${line#Mnemonic: }"; fi
    if [[ "$line" == Alphanumeric:\ * ]]; then alphanumeric="${line#Alphanumeric: }"; fi
  done <<< "$out"
  echo "$mnemonic"
  echo "$alphanumeric"
}

# Парсинг sign-message: Public Key та Compact (перший блок Main Signature). Вивід: два рядки (pubkey, compact).
parse_sign_output() {
  local out=$1
  local pubkey="" compact=""
  local in_main=0
  while IFS= read -r line; do
    line=$(echo "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    if [[ "$line" == "Main Signature:" ]]; then in_main=1; continue; fi
    if [[ "$in_main" -eq 1 ]]; then
      if [[ "$line" == Public\ Key:\ * ]]; then pubkey="${line#Public Key: }"; fi
      if [[ "$line" == Compact:\ * ]]; then compact="${line#Compact: }"; fi
      if [[ "$line" == "HKDF Signature:" ]]; then break; fi
    fi
  done <<< "$out"
  # compact: лише 128 hex (Core очікує compact)
  compact=$(echo "$compact" | tr -dc '0-9a-fA-F')
  if [[ ${#compact} -gt 128 ]]; then compact="${compact:0:128}"; fi
  echo "$pubkey"
  echo "$compact"
}

echo "=== Platarium Load Test: TX 1→2, 2→1 (розподіл по мережі) ==="
echo "Nodes:       $NODES (TX round-robin: кожна TX на чергову ноду)"
echo "Rounds:      $ROUNDS (2 TX per round)"
echo "First node:  $BASE_URL"
echo ""

# Рахунок 1
echo "Generating account 1..."
out1=$("$CLI" generate-mnemonic 2>/dev/null)
mnemonic1=$(parse_mnemonic_output "$out1" | head -1)
alphanumeric1=$(parse_mnemonic_output "$out1" | tail -1)
[ -n "$mnemonic1" ] && [ -n "$alphanumeric1" ] || { echo "Failed to generate account 1"; exit 1; }
echo "  Address 1: $alphanumeric1"

# Рахунок 2
echo "Generating account 2..."
out2=$("$CLI" generate-mnemonic 2>/dev/null)
mnemonic2=$(parse_mnemonic_output "$out2" | head -1)
alphanumeric2=$(parse_mnemonic_output "$out2" | tail -1)
[ -n "$mnemonic2" ] && [ -n "$alphanumeric2" ] || { echo "Failed to generate account 2"; exit 1; }
echo "  Address 2: $alphanumeric2"
echo ""

# Дочекатися готовності хоча б однієї ноди
echo "Waiting for gateway at $BASE_URL..."
for attempt in $(seq 1 25); do
  code=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 2 --max-time 5 "$BASE_URL/api" 2>/dev/null) || code=""
  if [ "$code" = "200" ]; then
    echo "  Gateway ready."
    break
  fi
  [ "$attempt" -eq 25 ] && { echo "  ERROR: Gateway not responding. Start nodes first (run_100nodes.sh)."; exit 1; }
  sleep 1
done

# Скільки нод підряд від 0 відповідають - round-robin тільки по них (ноди 0,1,...,NODES-1)
echo "Detecting nodes (checking ports $BASE_PORT, $((BASE_PORT+10)), ... until first timeout):"
detected=0
for (( i=0; i<100; i++ )); do
  port=$((BASE_PORT+10*i))
  code=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 1 --max-time 2 "http://localhost:$port/api" 2>/dev/null) || code=""
  if [ "$code" = "200" ]; then
    echo "  Node $i (port $port): OK"
    (( detected++ )) || true
  else
    [ -z "$code" ] && code="таймаут/відмова"
    echo "  Node $i (port $port): немає ($code)"
    break
  fi
done
if [ "$detected" -eq 0 ]; then
  echo "  ERROR: Жодної ноди не відповідає. Запусти gateway: ./scripts/run_100nodes.sh або одна нода на порту $BASE_PORT."
  exit 1
fi
NODES=$detected
echo "  - Підсумок: $NODES нод(и) (порти $BASE_PORT..$((BASE_PORT+10*(NODES-1)))). TX round-robin лише по них."
[ "$NODES" -eq 1 ] && echo "  (Одна нода - усі TX на неї. Для тесту розподілу спочатку запусти: ./scripts/run_100nodes.sh)"
echo ""

# Канонічний порядок полів (як у CoreVerifyMessage)
build_msg() {
  local from=$1 to=$2 value=$3 nonce=$4 ts=$5
  echo "{\"from\":\"$from\",\"to\":\"$to\",\"value\":\"$value\",\"nonce\":$nonce,\"timestamp\":$ts,\"type\":\"transfer\"}"
}

# $1 = URL ноди (http://localhost:PORT), решта - параметри TX
send_tx() {
  local url=$1 from_addr=$2 to_addr=$3 value=$4 nonce=$5 mnemonic=$6 alphanumeric=$7
  local ts=$(date +%s)
  local msg
  msg=$(build_msg "$from_addr" "$to_addr" "$value" "$nonce" "$ts")
  local sign_out
  sign_out=$("$CLI" sign-message --message "$msg" --mnemonic "$mnemonic" --alphanumeric "$alphanumeric" 2>/dev/null) || return 1
  local parsed pubkey compact
  parsed=$(parse_sign_output "$sign_out")
  pubkey=$(echo "$parsed" | head -1)
  compact=$(echo "$parsed" | tail -1)
  [ -n "$pubkey" ] && [ -n "$compact" ] || return 1
  local body
  body=$(cat <<EOF
{"from":"$from_addr","to":"$to_addr","amount":"$value","value":"$value","nonce":$nonce,"timestamp":$ts,"type":"transfer","signature":"$compact","pubkey":"$pubkey"}
EOF
)
  echo -n "" > /tmp/tx_resp.json
  echo -n "" > /tmp/tx_code.txt
  local code
  code=$(curl -s -o /tmp/tx_resp.json -w "%{http_code}" --connect-timeout 3 --max-time 10 -X POST "$url/pg-sendtx" -H "Content-Type: application/json" -d "$body")
  [ "$code" != "200" ] && echo -n "$code" > /tmp/tx_code.txt
  if [ "$code" = "200" ]; then return 0; else return 1; fi
}

ok_1to2=0 ok_2to1=0 fail_1to2=0 fail_2to1=0
ok_fallback_1to2=0 ok_fallback_2to1=0
start=$(date +%s.%N)
first_fail_msg=""
node0_url="http://localhost:$BASE_PORT"

echo "Sending transactions (round-robin across nodes 0..$((NODES-1)), so TX enter the network from different nodes)..."
for (( r=0; r<ROUNDS; r++ )); do
  node_idx=$(( r % NODES ))
  node_url="http://localhost:$((BASE_PORT + 10*node_idx))"
  [ "$r" -eq 0 ] && echo "  Round 1: node $node_idx ($node_url), signing and sending..."
  if send_tx "$node_url" "$alphanumeric1" "$alphanumeric2" "50" "$r" "$mnemonic1" "$alphanumeric1"; then
    (( ok_1to2++ )) || true
  else
    if send_tx "$node0_url" "$alphanumeric1" "$alphanumeric2" "50" "$r" "$mnemonic1" "$alphanumeric1"; then
      (( ok_fallback_1to2++ )) || true
    else
      (( fail_1to2++ )) || true
      if [ -z "$first_fail_msg" ]; then
        hcode=$(cat /tmp/tx_code.txt 2>/dev/null); [ -z "$hcode" ] && hcode="-"
        first_fail_msg="1→2 round $((r+1)) node $node_idx: HTTP $hcode | $(cat /tmp/tx_resp.json 2>/dev/null | head -c 100)"
      fi
    fi
  fi
  node_idx2=$(( (r + 1) % NODES ))
  node_url2="http://localhost:$((BASE_PORT + 10*node_idx2))"
  if send_tx "$node_url2" "$alphanumeric2" "$alphanumeric1" "50" "$r" "$mnemonic2" "$alphanumeric2"; then
    (( ok_2to1++ )) || true
  else
    if send_tx "$node0_url" "$alphanumeric2" "$alphanumeric1" "50" "$r" "$mnemonic2" "$alphanumeric2"; then
      (( ok_fallback_2to1++ )) || true
    else
      (( fail_2to1++ )) || true
      if [ -z "$first_fail_msg" ]; then
        hcode=$(cat /tmp/tx_code.txt 2>/dev/null); [ -z "$hcode" ] && hcode="-"
        first_fail_msg="2→1 round $((r+1)) node $node_idx2: HTTP $hcode | $(cat /tmp/tx_resp.json 2>/dev/null | head -c 100)"
      fi
    fi
  fi
  # Прогрес кожні 5 раундів та обов'язково на першому
  if [[ $(( (r+1) % 5 )) -eq 0 ]] || [[ $r -eq 0 ]]; then
    echo "  Round $((r+1))/$ROUNDS - OK 1→2: $ok_1to2, 2→1: $ok_2to1 | fallback: $ok_fallback_1to2/$ok_fallback_2to1 | Fail: $fail_1to2/$fail_2to1"
  fi
done
[ -n "$first_fail_msg" ] && echo "  First failure: $first_fail_msg"

end=$(date +%s.%N)
elapsed=$(echo "$end - $start" | bc 2>/dev/null || echo "0")
total_ok=$(( ok_1to2 + ok_2to1 + ok_fallback_1to2 + ok_fallback_2to1 ))
total_fail=$(( fail_1to2 + fail_2to1 ))

total_fallback=$(( ok_fallback_1to2 + ok_fallback_2to1 ))
echo ""
echo "========== Статистика =========="
echo "  Транзакції 1→2:  OK=$ok_1to2  (через fallback на ноду 0: $ok_fallback_1to2)  FAIL=$fail_1to2"
echo "  Транзакції 2→1:  OK=$ok_2to1  (через fallback на ноду 0: $ok_fallback_2to1)  FAIL=$fail_2to1"
echo "  Всього успішно:  $total_ok"
[ "$total_fallback" -gt 0 ] && echo "  Успішно через fallback (нода не відповіла - відправлено на ноду 0): $total_fallback"
echo "  Всього помилок:  $total_fail"
echo "  Час (с):         ${elapsed}"
if command -v bc &>/dev/null && [ -n "$elapsed" ] && [ "$(echo "$elapsed > 0" | bc)" -eq 1 ]; then
  tps=$(echo "scale=2; $total_ok / $elapsed" | bc 2>/dev/null)
  echo "  TX/s (успішні):  $tps"
fi
echo "================================="

# Перевірка розповсюдження: баланси на кількох нодах
echo ""
echo "Перевірка розповсюдження (баланси на різних нодах):"
for idx in $(echo "$SAMPLE_NODES" | tr ',' ' '); do
  port=$((BASE_PORT+10*idx))
  url="http://localhost:$port"
  bal1=$(curl -s "$url/pg-bal/$alphanumeric1" 2>/dev/null | grep -o '"balance":"[^"]*"' | head -1 | sed 's/"balance":"//;s/"//') || bal1="?"
  bal2=$(curl -s "$url/pg-bal/$alphanumeric2" 2>/dev/null | grep -o '"balance":"[^"]*"' | head -1 | sed 's/"balance":"//;s/"//') || bal2="?"
  echo "  Node $idx (port $port): addr1=$bal1  addr2=$bal2"
done
echo ""
echo "Done."
