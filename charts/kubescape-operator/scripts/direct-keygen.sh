#!/usr/bin/env bash
set -euo pipefail
PUB_NS=${PUB_NS:?}
PRIV_NS=${PRIV_NS:?}
PRIV=node-agent-direct-jwt
PUB=node-agent-direct-jwt-pub

has() { kubectl -n "$1" get secret "$2" >/dev/null 2>&1; }
kubectl get namespace "$PRIV_NS" >/dev/null 2>&1 || kubectl create namespace "$PRIV_NS"

if has "$PRIV_NS" "$PRIV"; then
  if has "$PUB_NS" "$PUB"; then
    echo "keypair present, nothing to do"
    exit 0
  fi
  echo "public half missing, deriving it from the existing private key"
  kubectl -n "$PRIV_NS" get secret "$PRIV" -o jsonpath='{.data.key\.pem}' | base64 -d \
    | openssl ec -pubout 2>/dev/null \
    | kubectl -n "$PUB_NS" create secret generic "$PUB" --from-file=public.pem=/dev/stdin
  exit 0
fi

echo "minting a new ES256 keypair"
key=$(openssl ecparam -name prime256v1 -genkey -noout)
printf '%s\n' "$key" | kubectl -n "$PRIV_NS" create secret generic "$PRIV" --from-file=key.pem=/dev/stdin
has "$PUB_NS" "$PUB" && kubectl -n "$PUB_NS" delete secret "$PUB"
printf '%s\n' "$key" | openssl ec -pubout 2>/dev/null \
  | kubectl -n "$PUB_NS" create secret generic "$PUB" --from-file=public.pem=/dev/stdin
