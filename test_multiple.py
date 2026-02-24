#!/usr/bin/env python3
"""Imprime plusieurs tickets de suite pour tester les doublons."""

from escpos.printer import Usb
from datetime import datetime
import time

VENDOR_ID = 0x04b8   # Epson
PRODUCT_ID = 0x0e28  # TM-T20III

def print_receipt(printer, num):
    """Imprime un ticket de test."""
    now = datetime.now()

    printer.hw('INIT')

    # En-tête
    printer.set(align='center')
    printer.text(f"=== TICKET TEST #{num} ===\n")
    printer.text("Magasin de Test\n")
    printer.text("123 Rue Example, 75001 Paris\n")
    printer.text("Tel: 01 23 45 67 89\n")
    printer.text("TVA: FR12345678901\n")
    printer.text("\n")

    # Infos
    printer.set(align='left')
    printer.text(f"Date: {now.strftime('%d/%m/%Y %H:%M:%S')}\n")
    printer.text(f"Ticket: TKT-{num:04d}\n")
    printer.text("\n")

    # Header tableau
    printer.set(bold=True)
    printer.text("ARTICLE              QTE  PRIX\n")
    printer.set(bold=False)
    printer.text("--------------------------------\n")

    # Articles
    articles = [
        ("Pomme Bio", 3, "2.50"),
        ("Pain Complet", 2, "1.80"),
        ("Fromage Comte", 1, "8.90"),
        ("Eau 1L", 4, "0.75"),
    ]

    for name, qty, price in articles:
        name_padded = name[:20].ljust(20)
        line = f"{name_padded} {qty:3d} {price:>6s}\n"
        printer.text(line)

    printer.text("--------------------------------\n")

    # Total
    printer.set(align='right', bold=True)
    printer.text("TOTAL:      17.45 EUR\n")
    printer.set(bold=False)
    printer.text("\n")

    # Footer
    printer.set(align='center')
    printer.text("PAIEMENT: Especes\n\n")
    printer.text("Merci de votre visite!\n")
    printer.text("www.exemple.com\n\n\n")

    # Coupe
    printer.cut()

    print(f"  ✓ Ticket #{num} imprimé")


def main():
    """Imprime plusieurs tickets."""
    nb_tickets = 5

    print(f"Impression de {nb_tickets} tickets de test...")
    print("=" * 50)

    try:
        printer = Usb(VENDOR_ID, PRODUCT_ID)
        print("✓ Connecté à l'imprimante\n")

        for i in range(1, nb_tickets + 1):
            print(f"Impression ticket {i}/{nb_tickets}...")
            print_receipt(printer, i)

            # Petite pause entre les tickets
            if i < nb_tickets:
                time.sleep(0.5)

        print("\n" + "=" * 50)
        print(f"✓ {nb_tickets} tickets imprimés avec succès!")
        print("\nVérifiez CHAQUE ticket pour des lignes doublées.")
        print("Notez les numéros des tickets qui ont des doublons.")

    except Exception as e:
        print(f"Erreur: {e}")
        return 1

    return 0


if __name__ == "__main__":
    exit(main())
