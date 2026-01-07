#!/usr/bin/env python3
import subprocess
from PIL import Image, ImageDraw, ImageFont
import random
import sys
import string
import os

def calculate_ean13_checksum(code_12_digits):
    total = 0
    for i, digit in enumerate(code_12_digits):
        if i % 2 == 0:
            total += int(digit)
        else:
            total += int(digit) * 3
    return str((10 - (total % 10)) % 10)

def generate_internal_ean13():
    prefix = "2"
    random_part = "".join([str(random.randint(0, 9)) for _ in range(11)])
    code_12 = prefix + random_part
    checksum = calculate_ean13_checksum(code_12)
    return code_12 + checksum

def generate_internal_code128():
    """Génère un code-barres Code 128 aléatoire avec des lettres et chiffres"""
    chars = string.ascii_uppercase + string.digits
    return "INT" + "".join(random.choices(chars, k=8))

def get_code128_patterns():
    """Retourne les patterns Code 128"""
    patterns = {
        # Code Set A (caractères de contrôle, chiffres, lettres majuscules)
        'A': {
            ' ': '11011001100', '!': '11001101100', '"': '11001100110', '#': '10010011000',
            '$': '10010001100', '%': '10001001100', '&': '10011001000', "'": '10011000100',
            '(': '10001100100', ')': '11001001000', '*': '11001000100', '+': '11000100100',
            ',': '10110011100', '-': '10011011100', '.': '10011001110', '/': '10111001100',
            '0': '10011101100', '1': '10011100110', '2': '11001110010', '3': '11001011100',
            '4': '11001001110', '5': '11011100100', '6': '11001110100', '7': '11101101110',
            '8': '11101001100', '9': '11100101100', ':': '11100100110', ';': '11101100100',
            '<': '11100110100', '=': '11100110010', '>': '11011011000', '?': '11011000110',
            '@': '11000110110', 'A': '10100011000', 'B': '10001011000', 'C': '10001000110',
            'D': '10110001000', 'E': '10001101000', 'F': '10001100010', 'G': '11010001000',
            'H': '11000101000', 'I': '11000100010', 'J': '10110111000', 'K': '10110001110',
            'L': '10001101110', 'M': '10111011000', 'N': '10111000110', 'O': '10001110110',
            'P': '11101110110', 'Q': '11010001110', 'R': '11000101110', 'S': '11011101000',
            'T': '11011100010', 'U': '11011101110', 'V': '11101011000', 'W': '11101000110',
            'X': '11100010110', 'Y': '11101101000', 'Z': '11101100010'
        }
    }
    
    # Codes spéciaux
    start_a = '11010000100'
    start_b = '11010010000'
    start_c = '11010011100'
    stop = '1100011101011'
    
    return patterns, start_a, start_b, start_c, stop

def calculate_code128_checksum(data, start_code):
    """Calcule le checksum pour Code 128"""
    # Valeurs des caractères pour le checksum
    char_values = {}
    
    # Valeurs pour les caractères ASCII imprimables
    for i in range(32, 127):
        char_values[chr(i)] = i - 32
    
    # Start codes
    start_values = {'A': 103, 'B': 104, 'C': 105}
    
    checksum = start_values[start_code]
    
    for i, char in enumerate(data):
        if char in char_values:
            checksum += char_values[char] * (i + 1)
    
    return checksum % 103

def get_checksum_pattern(checksum_value):
    """Retourne le pattern pour la valeur de checksum"""
    patterns = [
        '11011001100', '11001101100', '11001100110', '10010011000', '10010001100',
        '10001001100', '10011001000', '10011000100', '10001100100', '11001001000',
        '11001000100', '11000100100', '10110011100', '10011011100', '10011001110',
        '10111001100', '10011101100', '10011100110', '11001110010', '11001011100',
        '11001001110', '11011100100', '11001110100', '11101101110', '11101001100',
        '11100101100', '11100100110', '11101100100', '11100110100', '11100110010',
        '11011011000', '11011000110', '11000110110', '10100011000', '10001011000',
        '10001000110', '10110001000', '10001101000', '10001100010', '11010001000',
        '11000101000', '11000100010', '10110111000', '10110001110', '10001101110',
        '10111011000', '10111000110', '10001110110', '11101110110', '11010001110',
        '11000101110', '11011101000', '11011100010', '11011101110', '11101011000',
        '11101000110', '11100010110', '11101101000', '11101100010', '11100011010',
        '11101111010', '11001000010', '11110001010', '10100110000', '10100001100',
        '10010110000', '10010000110', '10000101100', '10000100110', '10110010000',
        '10110000100', '10011010000', '10011000010', '10000110100', '10000110010',
        '11000010010', '11001010000', '11110111010', '11000010100', '10001111010',
        '10100111100', '10010111100', '10010011110', '10111100100', '10011110100',
        '10011110010', '11110100100', '11110010100', '11110010010', '11011011110',
        '11011110110', '11110110110', '10101111000', '10100011110', '10001011110',
        '10111101000', '10111100010', '11110101000', '11110100010', '10111011110',
        '10111101110', '11101011110', '11110101110', '11010000100', '11010010000',
        '11010011100', '1100011101011'
    ]
    
    if 0 <= checksum_value < len(patterns):
        return patterns[checksum_value]
    return patterns[0]

def draw_code128_barcode(draw, x, y, width, height, data, module_width):
    """Dessine un code-barres Code 128"""
    patterns, start_a, start_b, start_c, stop = get_code128_patterns()
    
    # Utilise le Code Set A pour supporter les lettres majuscules et chiffres
    current_x = x
    
    # Start code A
    for bit in start_a:
        if bit == '1':
            draw.rectangle([current_x, y, current_x + module_width, y + height], fill='black')
        current_x += module_width
    
    # Données
    for char in data:
        if char in patterns['A']:
            pattern = patterns['A'][char]
            for bit in pattern:
                if bit == '1':
                    draw.rectangle([current_x, y, current_x + module_width, y + height], fill='black')
                current_x += module_width
        else:
            # Si le caractère n'est pas supporté, le remplacer par un espace
            pattern = patterns['A'][' ']
            for bit in pattern:
                if bit == '1':
                    draw.rectangle([current_x, y, current_x + module_width, y + height], fill='black')
                current_x += module_width
    
    # Checksum
    checksum_value = calculate_code128_checksum(data, 'A')
    checksum_pattern = get_checksum_pattern(checksum_value)
    for bit in checksum_pattern:
        if bit == '1':
            draw.rectangle([current_x, y, current_x + module_width, y + height], fill='black')
        current_x += module_width
    
    # Stop code
    for bit in stop:
        if bit == '1':
            draw.rectangle([current_x, y, current_x + module_width, y + height], fill='black')
        current_x += module_width
    
    return True

def draw_ean13_barcode(draw, x, y, width, height, ean13_code, module_width):
    """Dessine un code-barres EAN-13 (code original conservé)"""
    patterns = {
        'A': {
            '0': '0001101', '1': '0011001', '2': '0010011', '3': '0111101',
            '4': '0100011', '5': '0110001', '6': '0101111', '7': '0111011',
            '8': '0110111', '9': '0001011'
        },
        'B': {
            '0': '0100111', '1': '0110011', '2': '0011011', '3': '0100001',
            '4': '0011101', '5': '0111001', '6': '0000101', '7': '0010001',
            '8': '0001001', '9': '0010111'
        },
        'C': {
            '0': '1110010', '1': '1100110', '2': '1101100', '3': '1000010',
            '4': '1011100', '5': '1001110', '6': '1010000', '7': '1000100',
            '8': '1001000', '9': '1110100'
        }
    }
    
    first_digit_patterns = {
        '0': 'AAAAAA', '1': 'AABABB', '2': 'AABBAB', '3': 'AABBBA',
        '4': 'ABAABB', '5': 'ABBAAB', '6': 'ABBBAA', '7': 'ABABAB',
        '8': 'ABABBA', '9': 'ABBABA'
    }
    
    if len(ean13_code) != 13:
        return False
    
    first_digit = ean13_code[0]
    left_digits = ean13_code[1:7]
    right_digits = ean13_code[7:13]
    pattern_sequence = first_digit_patterns[first_digit]
    
    current_x = x
    
    for bit in '101':
        if bit == '1':
            draw.rectangle([current_x, y, current_x + module_width, y + height], fill='black')
        current_x += module_width
    
    for i, digit in enumerate(left_digits):
        pattern_type = pattern_sequence[i]
        bit_pattern = patterns[pattern_type][digit]
        
        for bit in bit_pattern:
            if bit == '1':
                draw.rectangle([current_x, y, current_x + module_width, y + height], fill='black')
            current_x += module_width
    
    for bit in '01010':
        if bit == '1':
            draw.rectangle([current_x, y, current_x + module_width, y + height], fill='black')
        current_x += module_width
    
    for digit in right_digits:
        bit_pattern = patterns['C'][digit]
        
        for bit in bit_pattern:
            if bit == '1':
                draw.rectangle([current_x, y, current_x + module_width, y + height], fill='black')
            current_x += module_width
    
    for bit in '101':
        if bit == '1':
            draw.rectangle([current_x, y, current_x + module_width, y + height], fill='black')
        current_x += module_width
    
    return True

def is_numeric(barcode):
    """Vérifie si le code-barres ne contient que des chiffres"""
    return barcode.isdigit()

def format_barcode_display(barcode_code, is_ean13=False):
    """Formate l'affichage du code-barres"""
    if is_ean13 and len(barcode_code) == 13:
        return f"{barcode_code[0]} {barcode_code[1:7]} {barcode_code[7:13]}"
    return barcode_code

def create_address_label(line1, line2, barcode_number, line3="", height=300):
    """Create an address label without price and barcode"""
    width = 696
    
    # Vérifier si sticker.png existe à la racine du projet
    sticker_path = "./sticker.png"
    if os.path.exists(sticker_path):
        try:
            # Charger l'image sticker.png
            img = Image.open(sticker_path)
            
            # S'assurer qu'elle fait la bonne taille (696x300)
            if img.size != (width, height):
                # Redimensionner si nécessaire
                img = img.resize((width, height), Image.Resampling.LANCZOS if hasattr(Image, 'Resampling') else Image.LANCZOS)
            
            # Convertir en noir et blanc pour l'impression
            bw = img.convert('1')
            filename = f"etiquette_sticker.png"
            bw.save(filename)
            
            return filename, "STICKER"
        except Exception as e:
            print(f"Erreur lors du chargement de sticker.png: {e}, création d'une étiquette texte", file=sys.stderr)
            # Continuer avec l'étiquette texte par défaut
    
    # Si pas de sticker.png ou erreur, créer l'étiquette texte normale
    image = Image.new('RGB', (width, height), color='white')
    draw = ImageDraw.Draw(image)
    
    try:
        font_large = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 36)
        font_medium = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 28)
        font_small = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 24)
    except:
        font_large = ImageFont.load_default()
        font_medium = ImageFont.load_default()
        font_small = ImageFont.load_default()
    
    margin = 20
    current_y = margin + 30
    
    # Line 1 - Company name (bold, larger)
    bbox = draw.textbbox((0, 0), line1, font=font_large)
    text_width = bbox[2] - bbox[0]
    x_centered = (width - text_width) // 2
    draw.text((x_centered, current_y), line1, fill='black', font=font_large)
    current_y += 60
    
    # Line 2 - Address
    if line2:
        bbox = draw.textbbox((0, 0), line2, font=font_medium)
        text_width = bbox[2] - bbox[0]
        x_centered = (width - text_width) // 2
        draw.text((x_centered, current_y), line2, fill='black', font=font_medium)
        current_y += 50
    
    # Line 3 - City/Postal code
    if line3 and line3.strip():
        bbox = draw.textbbox((0, 0), line3, font=font_medium)
        text_width = bbox[2] - bbox[0]
        x_centered = (width - text_width) // 2
        draw.text((x_centered, current_y), line3, fill='black', font=font_medium)
    
    # Bordure
    draw.rectangle([2, 2, width-2, height-2], outline='black', width=1)
    
    # Sauvegarde
    bw = image.convert('1')
    filename = f"etiquette_address.png"
    bw.save(filename)
    
    return filename, "ADDRESS"

def create_price_label(product_name, price_euros, barcode_number, footer="", height=300):
    width = 696
    
    # Détermine le type de code-barres à générer
    use_ean13 = False
    
    if barcode_number is None or barcode_number == "":
        # Génère un code aléatoire basé sur le type demandé
        barcode_code = generate_internal_code128()
        use_ean13 = False
    else:
        if is_numeric(barcode_number):
            # Code numérique - utilise EAN-13 si possible
            if len(barcode_number) == 12:
                barcode_code = barcode_number + calculate_ean13_checksum(barcode_number)
                use_ean13 = True
            elif len(barcode_number) == 13:
                barcode_code = barcode_number
                use_ean13 = True
            else:
                # Code numérique mais pas de la bonne longueur pour EAN-13
                barcode_code = barcode_number
                use_ean13 = False
        else:
            # Code alphanumérique - utilise Code 128
            barcode_code = barcode_number.upper()  # Convertit en majuscules
            use_ean13 = False
    
    image = Image.new('RGB', (width, height), color='white')
    draw = ImageDraw.Draw(image)
    
    try:
        font_large = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 32)
        font_medium = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 22)
        font_small = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 20)
        font_price = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 40)
        font_website = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 22)
    except:
        font_large = ImageFont.load_default()
        font_medium = ImageFont.load_default()
        font_small = ImageFont.load_default()
        font_price = ImageFont.load_default()
        font_website = ImageFont.load_default()
    
    margin = 12
    current_y = margin
    
    # Traitement du nom du produit
    max_chars_per_line = 32
    lines = []
    
    truncated_name = product_name
    if len(product_name) > max_chars_per_line * 2:
        truncated_name = product_name[:max_chars_per_line * 2 - 3] + "..."
    
    if len(truncated_name) > max_chars_per_line:
        words = truncated_name.split()
        current_line = ""
        for word in words:
            if len(current_line + word + " ") <= max_chars_per_line:
                current_line += word + " "
            else:
                if current_line:
                    lines.append(current_line.strip())
                    if len(lines) >= 2:
                        break
                current_line = word + " "
        if current_line and len(lines) < 2:
            lines.append(current_line.strip())
    else:
        lines = [truncated_name]
    
    for line in lines:
        bbox = draw.textbbox((0, 0), line, font=font_medium)
        text_width = bbox[2] - bbox[0]
        x_centered = (width - text_width) // 2
        draw.text((x_centered, current_y), line, fill='black', font=font_medium)
        current_y += 24
    
    current_y += 15
    
    # Dessin du code-barres
    barcode_height = 100
    module_width = 3 if not use_ean13 else 5
    
    if use_ean13:
        # Code-barres EAN-13
        actual_barcode_width = 113 * module_width
        base_center = (width - actual_barcode_width) // 2
        barcode_x = base_center + 35
        success = draw_ean13_barcode(draw, barcode_x, current_y, actual_barcode_width, barcode_height, barcode_code, module_width)
    else:
        # Code-barres Code 128
        # Estimation de la largeur (11 bits par caractère + start + checksum + stop)
        estimated_width = (len(barcode_code) + 3) * 11 * module_width
        barcode_x = (width - estimated_width) // 2
        success = draw_code128_barcode(draw, barcode_x, current_y, estimated_width, barcode_height, barcode_code, module_width)
    
    if not success:
        draw.text((barcode_x, current_y), f"Code: {barcode_code}", fill='black', font=font_small)
    
    current_y += barcode_height + 5
    
    # Affichage du code-barres
    formatted_barcode = format_barcode_display(barcode_code, use_ean13)
    bbox = draw.textbbox((0, 0), formatted_barcode, font=font_small)
    text_width = bbox[2] - bbox[0]
    x_centered = (width - text_width) // 2
    draw.text((x_centered, current_y), formatted_barcode, fill='black', font=font_small)
    current_y += 30
    
    # Prix
    price_text = f"{price_euros:.2f} €"
    bbox = draw.textbbox((0, 0), price_text, font=font_price)
    text_width = bbox[2] - bbox[0]
    x_centered = (width - text_width) // 2
    draw.text((x_centered, current_y), price_text, fill='black', font=font_price)
    current_y += 50
    
    # Pied de page
    if footer and footer.strip():
        bbox = draw.textbbox((0, 0), footer, font=font_website)
        text_width = bbox[2] - bbox[0]
        x_centered = (width - text_width) // 2
        draw.text((x_centered, current_y), footer, fill='black', font=font_website)
    
    # Bordure
    draw.rectangle([2, 2, width-2, height-2], outline='black', width=1)
    
    # Sauvegarde
    bw = image.convert('1')
    safe_barcode = "".join(c for c in barcode_code if c.isalnum())
    filename = f"etiquette_{safe_barcode}.png"
    bw.save(filename)
    
    return filename, barcode_code

def print_label(filename):
    cmd = [
        'brother_ql', '--backend', 'linux_kernel',
        '--model', 'QL-800',
        '--printer', '/dev/usb/brother_ql800',
        'print', '-l', '62', filename
    ]
    
    result = subprocess.run(cmd, capture_output=True, text=True)
    
    if 'Total:' in result.stderr:
        return True, "Print successful"
    else:
        return False, f"Print error: {result.stderr.strip()}"

def print_sticker_image(image_path):
    """Print a sticker image directly"""
    try:
        # Open and prepare the image
        img = Image.open(image_path)
        
        # Convert to proper format if needed
        if img.mode == 'RGBA':
            background = Image.new('RGB', img.size, (255, 255, 255))
            background.paste(img, mask=img.split()[3])
            img = background
        elif img.mode != 'RGB':
            img = img.convert('RGB')
        
        # Resize if too wide (696px is the printable width for 62mm labels)
        if img.width > 696:
            ratio = 696 / img.width
            new_height = int(img.height * ratio)
            img = img.resize((696, new_height), Image.Resampling.LANCZOS if hasattr(Image, 'Resampling') else Image.LANCZOS)
        
        # Convert to 1-bit black and white
        img = img.convert('1')
        
        # Save to temporary file
        temp_filename = f"/tmp/sticker_{os.getpid()}.png"
        img.save(temp_filename)
        
        # Print using brother_ql
        success, message = print_label(temp_filename)
        
        # Clean up
        try:
            os.remove(temp_filename)
        except:
            pass
        
        return success, message
    except Exception as e:
        return False, f"Error processing sticker: {str(e)}"

def main():
    try:
        if len(sys.argv) < 4:
            print("Usage: python3 etiquettes.py <title> <price> <barcode> [footer]", file=sys.stderr)
            print("Exemples:", file=sys.stderr)
            print("  python3 etiquettes.py 'Python Book' 29.90 210012345 'www.site.com'  # EAN-13", file=sys.stderr)
            print("  python3 etiquettes.py 'Special Item' 15.50 'ABC123DEF' 'www.site.com'  # Code 128", file=sys.stderr)
            sys.exit(1)
        
        title = sys.argv[1]
        
        # Try to parse price as float, but if it fails, treat it as text (for address stickers)
        price_text = sys.argv[2]
        try:
            price = float(price_text)
            is_address_mode = False
        except ValueError:
            # Not a number, probably an address for sticker mode
            price = 0.0
            is_address_mode = True
        
        barcode = sys.argv[3]
        footer = sys.argv[4] if len(sys.argv) > 4 else ""
        
        # Validation moins restrictive - accepte maintenant les caractères alphanumériques
        if barcode and not all(c.isalnum() for c in barcode):
            print("Error: Barcode must contain only letters and digits", file=sys.stderr)
            sys.exit(1)
        
        # If in address mode, pass the address text in place of price
        if is_address_mode:
            # Create a modified version for address stickers
            filename, barcode_code = create_address_label(title, price_text, barcode, footer)
        else:
            filename, barcode_code = create_price_label(title, price, barcode, footer)
        
        if is_address_mode:
            print(f"Generated address label")
        else:
            barcode_type = "EAN-13" if is_numeric(barcode_code) and len(barcode_code) == 13 else "Code 128"
            print(f"Generated {barcode_type} barcode: {barcode_code}")
        
        success, message = print_label(filename)
        
        if success:
            print(f"✓ {message}")
            # Delete the label file after successful printing
            try:
                os.remove(filename)
            except OSError as e:
                print(f"Warning: Could not delete file {filename}: {e}", file=sys.stderr)
            sys.exit(0)
        else:
            print(f"✗ {message}", file=sys.stderr)
            sys.exit(1)
            
    except Exception as e:
        print(f"Error: {str(e)}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()