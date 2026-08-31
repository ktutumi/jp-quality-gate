#!/usr/bin/env bash
# Generic harness hook example.
# Feed the assistant's final text to stdin.
set -euo pipefail

set +e
result="$(jp-quality-gate --pretty)"
status=$?
set -e

printf '%s\n' "$result"

case "$status" in
  0) exit 0 ;;
  1)
    # Harness-specific layer should feed this JSON back to the LLM and request
    # a Japanese-only correction, then run the gate again (max 1-2 retries).
    exit 1
    ;;
  *) exit 2 ;;
esac
