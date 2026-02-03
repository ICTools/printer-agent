#!/usr/bin/env python3
"""
Script de test pour imprimer un ticket avec python-escpos.
Permet de comparer avec l'implémentation Go et diagnostiquer les problèmes.

Installation:
    pip install python-escpos pillow

Usage:
    python3 test_receipt_escpos.py
"""

from escpos.printer import Usb, File
from datetime import datetime
import sys

# Configuration de l'imprimante
# Pour Epson TM-T20III, les IDs USB sont généralement:
VENDOR_ID = 0x04b8   # Epson
PRODUCT_ID = 0x0e28  # TM-T20III (peut varier, vérifier avec lsusb)

# Alternative : utiliser un fichier device
DEVICE_PATH = "/dev/usb/epson_tmt20iii"


def print_test_receipt():
    """Imprime un ticket de test similaire au code Go."""

    # Essayer d'abord avec USB direct, sinon avec device file
    try:
        print("Tentative de connexion via USB...")
        printer = Usb(VENDOR_ID, PRODUCT_ID)
        print("✓ Connecté via USB")
    except Exception as e:
        print(f"Échec connexion USB: {e}")
        print(f"Tentative via device file {DEVICE_PATH}...")
        try:
            printer = File(DEVICE_PATH)
            print("✓ Connecté via device file")
        except Exception as e2:
            print(f"Échec device file: {e2}")
            print("\nPour trouver les IDs USB de votre imprimante:")
            print("  lsusb")
            print("\nPour utiliser le device file:")
            print(f"  Vérifier que {DEVICE_PATH} existe")
            sys.exit(1)

    # Initialiser l'imprimante
    printer.hw('INIT')

    # Logo (optionnel - commenter si pas de logo)
    # printer.image("/chemin/vers/logo.png", center=True)
    # printer.text("\n")

    # En-tête centré
    printer.set(align='center')
    printer.text("Magasin de Test\n")
    printer.text("123 Rue Example\n")
    printer.text("75001 Paris\n")
    printer.text("Tel: 01 23 45 67 89\n")
    printer.text("TVA: FR12345678901\n")
    printer.text("\n")

    # Informations ticket (aligné à gauche)
    printer.set(align='left')
    now = datetime.now()
    printer.text(f"Date: {now.strftime('%d/%m/%Y %H:%M')}\n")
    printer.text(f"Ticket: TKT{now.strftime('%Y%m%d%H%M%S')}\n")
    printer.text("\n")

    # En-tête tableau (bold)
    printer.set(bold=True)
    printer.text("ARTICLE              QTE  PRIX\n")
    printer.set(bold=False)
    printer.text("--------------------------------\n")

    # Articles
    articles = [
        ("Pomme Bio", 3, "2.50"),
        ("Pain Complet", 2, "1.80"),
        ("Fromage Comte", 1, "8.90"),
        ("Eau Minerale 1L", 4, "0.75"),
    ]

    for name, qty, price in articles:
        # Formater la ligne (nom tronqué à 20 chars)
        name_padded = name[:20].ljust(20)
        qty_str = f"{qty:3d}"
        price_str = f"{price:>6s}"
        printer.text(f"{name_padded} {qty_str} {price_str}\n")

    printer.text("--------------------------------\n")

    # Total (centré, bold)
    printer.set(align='right', bold=True)
    printer.text("TOTAL:      17.45 EUR\n")
    printer.set(bold=False)
    printer.text("\n")

    # Paiement (centré)
    printer.set(align='center')
    printer.text("PAIEMENT: Especes\n\n")
    printer.text("Merci de votre visite!\n")
    printer.text("@votre_instagram\n")
    printer.text("www.votre-site.com\n\n")

    # Code-barre
    barcode_data = f"TKT{now.strftime('%Y%m%d%H%M%S')}"
    try:
        printer.barcode(barcode_data, 'CODE128', height=100, width=3, pos='BELOW')
    except Exception as e:
        print(f"Avertissement: Échec impression code-barre: {e}")

    # Saut de lignes et coupe
    printer.text("\n\n\n")
    printer.cut()

    print("\n✓ Ticket imprimé avec succès!")
    print("\nVérifiez si des lignes sont doublées sur le ticket imprimé.")
    print("Si le problème persiste avec python-escpos, c'est l'imprimante/firmware.")


def print_simple_test():
    """Imprime un test simple pour vérifier la connexion."""
    try:
        printer = File(DEVICE_PATH)
        printer.text("Test simple\n")
        printer.text("Ligne 1\n")
        printer.text("Ligne 2\n")
        printer.text("Ligne 3\n")
        printer.text("\n\n")
        printer.cut()
        print("✓ Test simple imprimé")
    except Exception as e:
        print(f"Erreur: {e}")


if __name__ == "__main__":
    print("=" * 50)
    print("Test d'impression avec python-escpos")
    print("=" * 50)
    print()

    if len(sys.argv) > 1 and sys.argv[1] == "simple":
        print_simple_test()
    else:
        print_test_receipt()

    print()
    print("Pour un test simple:")
    print("  python3 test_receipt_escpos.py simple")
    print()
    print("Pour trouver votre imprimante:")
    print("  lsusb  # Chercher Epson")
    print("  ls -la /dev/usb/")
