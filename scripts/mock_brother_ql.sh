#!/usr/bin/env sh
set -eu

log_file="${MOCK_BROTHER_QL_LOG:-/tmp/brother_ql_mock.log}"

{
  printf "brother_ql mock invoked at %s\n" "$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  printf "args:"
  for arg in "$@"; do
    printf " %s" "$arg"
  done
  printf "\n"
} >> "$log_file"

if [ "${MOCK_BROTHER_QL_SUCCESS:-}" = "1" ]; then
  printf "Total: 1\n" >&2
  printf "Total: 1\n"
  exit 0
fi

printf "mock printer: no device\n" >&2
exit 1
