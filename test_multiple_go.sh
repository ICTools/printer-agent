#!/bin/bash
# Script pour imprimer plusieurs tickets de test avec le binaire Go

DEVICE="/dev/usb/lp0"
NB_TESTS=5

echo "=========================================="
echo "Test d'impression de $NB_TESTS tickets"
echo "=========================================="
echo

for i in $(seq 1 $NB_TESTS); do
    echo "Impression ticket $i/$NB_TESTS..."
    sudo RECEIPT_PRINTER_DEVICE=$DEVICE ./print-agent receipt-test

    if [ $? -eq 0 ]; then
        echo "  ✓ Ticket #$i imprimé"
    else
        echo "  ✗ Erreur ticket #$i"
    fi

    # Petite pause entre les tickets
    if [ $i -lt $NB_TESTS ]; then
        sleep 0.5
    fi
    echo
done

echo "=========================================="
echo "✓ $NB_TESTS tickets imprimés"
echo "=========================================="
echo
echo "Vérifiez CHAQUE ticket pour des lignes doublées."
echo "Comparez avec les tickets Python (qui fonctionnaient)."
