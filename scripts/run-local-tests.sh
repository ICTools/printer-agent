#!/usr/bin/env sh
set -eu

root_dir="$(cd "$(dirname "$0")/.." && pwd)"
bin_dir="$root_dir/.tmp/bin"
tmp_dir="$root_dir/.tmp"
mkdir -p "$bin_dir" "$tmp_dir"

cp "$root_dir/scripts/mock_brother_ql.sh" "$bin_dir/brother_ql"
chmod +x "$bin_dir/brother_ql"

printf "Building CLI...\n"
(
  cd "$root_dir"
  go build -o "$tmp_dir/print-agent" ./cmd/print-agent
)

printf "Testing receipt output (file device)...\n"
receipt_out="$tmp_dir/receipt.bin"
: > "$receipt_out"
RECEIPT_PRINTER_DEVICE="$receipt_out" "$tmp_dir/print-agent" receipt-test --barcode "TEST123"
printf "Receipt bytes written to %s\n" "$receipt_out"

printf "Testing drawer command (file device)...\n"
drawer_out="$tmp_dir/drawer.bin"
: > "$drawer_out"
"$tmp_dir/print-agent" open-drawer --device "$drawer_out"
printf "Drawer bytes written to %s\n" "$drawer_out"

printf "Testing label generation (mock brother_ql)...\n"
PATH="$bin_dir:$PATH" \
  LABEL_SCRIPT_PATH="$root_dir/scripts/print.py" \
  "$tmp_dir/print-agent" label "Livre" 12.90 9781234567890 --footer "Chapitre Neuf" || true
printf "Check %s for generated label PNG if mock failed.\n" "$root_dir"

printf "Testing sticker image (mock brother_ql)...\n"
PATH="$bin_dir:$PATH" \
  STICKER_SCRIPT_PATH="$root_dir/scripts/print_sticker.py" \
  "$tmp_dir/print-agent" sticker-image "$root_dir/scripts/sticker.png" || true

printf "Mock log: %s\n" "${MOCK_BROTHER_QL_LOG:-/tmp/brother_ql_mock.log}"
printf "Done.\n"
