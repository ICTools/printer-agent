# print-agent (Linux-only, CLI)

Minimal print agent extracted from the POS printing logic. Linux-only for now, CLI only.

## Installation

### System requirements

- Linux host with access to the printer device (e.g. `/dev/usb/*`, `/dev/lp*`)
- Python 3
- `brother_ql` CLI (for label/sticker printing on Brother QL)
- Pillow (Python imaging)
- Optional: ImageMagick `convert` (used for sticker resize in some workflows)

### Python dependencies

Example using pip:

```sh
python3 -m pip install pillow brother_ql
```

### Device permissions

You need write access to the printer device path. Example:

```sh
ls -l /dev/usb/lp0
```

If needed, add your user to the appropriate group (often `lp`):

```sh
sudo usermod -aG lp $USER
```

## Build

```sh
cd print-agent
go build ./cmd/print-agent
```

## Quick Start

```sh
cd print-agent
go build ./cmd/print-agent
./print-agent detect
./print-agent receipt-test --device /dev/usb/epson_tmt20iii
```

## Testing without a printer

You can exercise most of the code paths without hardware by writing to files and mocking `brother_ql`.

- Receipt and drawer (writes ESC/POS bytes to files):

```sh
RECEIPT_PRINTER_DEVICE=/tmp/receipt.bin ./print-agent receipt-test --barcode TEST123
./print-agent open-drawer --device /tmp/drawer.bin
```

- Labels/stickers (mock `brother_ql`, keep generated PNGs on failure):

```sh
export PATH="$(pwd)/scripts:$PATH"
chmod +x scripts/mock_brother_ql.sh
ln -sf scripts/mock_brother_ql.sh scripts/brother_ql
./print-agent label "Livre" 12.90 9781234567890 --footer "Chapitre Neuf" || true
./print-agent sticker-image ./scripts/sticker.png || true
```

- Or run the helper:

```sh
./scripts/run-local-tests.sh
```

## Commands

- Detect printers:

```sh
./print-agent detect
```

- Test receipt:

```sh
./print-agent receipt-test --device /dev/usb/epson_tmt20iii --logo /path/logo.png
```

- Open drawer:

```sh
./print-agent open-drawer --device /dev/usb/epson_tmt20iii
```

- Print price label:

```sh
./print-agent label "Livre" 12.90 9781234567890 --footer "Chapitre Neuf"
```

- Print address sticker:

```sh
./print-agent sticker-address "Chapitre Neuf" "21 Avenue des Combattants" "1370 Jodoigne"
```

- Print sticker image:

```sh
./print-agent sticker-image ./path/to/image.png
```

## Environment

- `RECEIPT_PRINTER_DEVICE`
- `RECEIPT_LOGO_PATH`
- `STORE_ADDRESS_LINE1`
- `STORE_ADDRESS_LINE2`
- `STORE_PHONE`
- `STORE_VAT_NUMBER`
- `STORE_SOCIAL_HANDLE`
- `STORE_WEBSITE`
- `PYTHON_PATH`
- `LABEL_SCRIPT_PATH` (defaults to `scripts/print.py`)
- `STICKER_SCRIPT_PATH` (defaults to `scripts/print_sticker.py`)

## Barcode notes

- Numeric barcode length 12 -> EAN-13 with checksum
- Numeric barcode length 13 -> EAN-13
- Alphanumeric -> Code128

## Troubleshooting

- `brother_ql` not found: ensure it is installed and in `PATH`.
- Permission denied on `/dev/usb/*`: check group membership and udev rules.
- Garbled text on receipts: ensure your content is ASCII-safe (accents are stripped).

## Notes

- Label/sticker printing uses `brother_ql` CLI and Pillow via `scripts/print.py`.
- `scripts/print.py` expects `sticker.png` in its working directory when printing the default sticker image.
