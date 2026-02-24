#!/usr/bin/env python3
"""Test simple avec connexion USB directe."""

from escpos.printer import Usb
import sys

VENDOR_ID = 0x04b8   # Epson
PRODUCT_ID = 0x0e28  # TM-T20III

try:
    print("Connexion à l'imprimante USB...")
    printer = Usb(VENDOR_ID, PRODUCT_ID)
    print("✓ Connecté!")

    print("Impression du test...")
    printer.text("=== TEST SIMPLE ===\n")
    printer.text("Ligne 1\n")
    printer.text("Ligne 2\n")
    printer.text("Ligne 3\n")
    printer.text("===================\n")
    printer.text("\n\n")
    printer.cut()

    print("✓ Test imprimé avec succès!")

except Exception as e:
    print(f"Erreur: {e}")
    print("\nSi 'Access denied', exécutez:")
    print("  sudo usermod -a -G lp $USER")
    print("  puis déconnectez-vous et reconnectez-vous")
    print("\nOu lancez avec sudo:")
    print("  sudo venv/bin/python3 test_simple.py")
    sys.exit(1)
