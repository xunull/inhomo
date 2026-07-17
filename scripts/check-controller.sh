#!/usr/bin/env bash
# check-controller.sh — 诊断 mihomo external-controller（TCP 或 Unix socket）。
# 以「运行中的内核进程」为准找出配置、控制器地址、secret，判断连通性，
# 给出可直接用的 inhomo 命令。全程只读，不改动任何配置。
#
# 不使用 set -u（兼容 macOS 自带 bash 3.2 的空数组展开）。
set -o pipefail

echo "════════ inhomo 前置检查：mihomo external-controller ════════"

# ---------- [1] 找运行中的内核进程 ----------
echo
echo "[1] 找运行中的内核进程（mihomo / clash-meta / verge-mihomo）："
PSLINE=$(ps -ax -o command= 2>/dev/null \
         | grep -E '(verge-mihomo|mihomo|clash-meta|clash\.meta)' \
         | grep -E ' -[fd] |-ext-ctl' | grep -v grep | head -1)
if [ -z "$PSLINE" ]; then
  echo "  ✖ 没发现运行中的内核进程。请先启动你的代理客户端，再跑本脚本。"
else
  echo "  · ${PSLINE:0:150}…"
fi

# 从进程参数抽运行配置(-f)与 Unix socket(-ext-ctl-unix)
CONF=$(echo "$PSLINE" | sed -nE 's/.* -f (.+\.ya?ml)( -.*|$)/\1/p' | head -1)
SOCK=$(echo "$PSLINE" | sed -nE 's/.*-ext-ctl-unix ([^ ]+).*/\1/p' | head -1)

# ---------- [2] 读控制器地址与 secret ----------
echo
echo "[2] 读取控制器地址与 secret："
ADDR=""; SECRET=""
if [ -n "$CONF" ] && [ -f "$CONF" ]; then
  echo "  · 运行配置：$CONF"
  ADDR=$(grep -E '^[[:space:]]*external-controller:' "$CONF" | head -1 \
    | sed -E 's/.*external-controller:[[:space:]]*//; s/[[:space:]]*#.*//; s#^https?://##; s/^["'\'']//; s/["'\'']$//')
  [ -z "$SOCK" ] && SOCK=$(grep -E '^[[:space:]]*external-controller-unix:' "$CONF" | head -1 \
    | sed -E 's/.*external-controller-unix:[[:space:]]*//; s/[[:space:]]*#.*//; s/^["'\'']//; s/["'\'']$//')
  SECRET=$(grep -E '^[[:space:]]*secret:' "$CONF" | head -1 \
    | sed -E 's/.*secret:[[:space:]]*//; s/[[:space:]]*#.*//; s/^["'\'']//; s/["'\'']$//')
else
  echo "  · （没从进程拿到配置文件路径，跳过配置读取）"
fi
echo "  · external-controller (TCP) ：${ADDR:-（空 / 未设）}"
echo "  · external-controller-unix  ：${SOCK:-（无）}"
echo "  · secret                    ：${SECRET:-（空）}"

# ---------- [3] 探测连通性 ----------
echo
echo "[3] 探测控制器是否响应（GET /version）："
AUTH=(); [ -n "$SECRET" ] && AUTH=(-H "Authorization: Bearer $SECRET")

TCP_OK=""; SOCK_OK=""
if [ -n "$ADDR" ]; then
  a=$(echo "$ADDR" | sed -E 's/^:/127.0.0.1:/; s/^0\.0\.0\.0:/127.0.0.1:/')
  c=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "${AUTH[@]}" "http://$a/version" 2>/dev/null)
  echo "  · TCP  http://$a/version → HTTP ${c:-无响应}"
  [ "$c" = "200" ] && TCP_OK="$a"
fi
if [ -n "$SOCK" ] && [ -S "$SOCK" ]; then
  c=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "${AUTH[@]}" --unix-socket "$SOCK" "http://localhost/version" 2>/dev/null)
  echo "  · Unix $SOCK → HTTP ${c:-无响应}"
  [ "$c" = "200" ] && SOCK_OK="$SOCK"
fi

# ---------- [4] 结论 ----------
echo
echo "[4] 结论与建议："
if [ -n "$TCP_OK" ]; then
  echo "  ✔ 有可用的 TCP 控制器，inhomo 可直接用："
  if [ -n "$SECRET" ]; then
    echo "      ./inhomo -controller $TCP_OK -secret '$SECRET' -out leaks.jsonl"
  else
    echo "      ./inhomo -controller $TCP_OK -out leaks.jsonl"
  fi
elif [ -n "$SOCK_OK" ]; then
  echo "  ⚠ 控制器只开在 Unix socket: $SOCK_OK  （TCP 那项是空的）"
  echo "    inhomo 目前只支持 TCP，暂时连不了 socket。两条路："
  echo "    (A) 在客户端里给内核开一个 TCP external-controller（如 127.0.0.1:9097），再跑 inhomo；或"
  echo "    (B) 给 inhomo 加 Unix socket 支持（推荐，正合这套 Clash Verge Rev）。"
  echo "    手动验证 socket 可用：curl --unix-socket $SOCK_OK ${SECRET:+-H \"Authorization: Bearer $SECRET\"} http://localhost/version"
else
  echo "  ✖ 没探到可用控制器。确认内核在跑，且配置里 external-controller 或 external-controller-unix 有值。"
fi
echo "══════════════════════════════════════════════════════════════"
