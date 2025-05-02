#!/bin/sh
[ $# -gt 0 ] || { echo "usage: $0 cmd [args...]" >&2; exit 1; }

while :; do
    nc -lk -p "${PORT:-4242}" -e "$@"
done
