#!/usr/bin/env python3
"""
Wrapper script for printing stickers with Brother QL printers.
Handles PIL/Pillow compatibility issues with brother_ql.
"""

import sys
import os
import subprocess
from PIL import Image
import tempfile

# Monkey patch for Pillow 10+ compatibility with brother_ql
if not hasattr(Image, 'ANTIALIAS'):
    Image.ANTIALIAS = Image.Resampling.LANCZOS

def prepare_image(image_path, width=696):
    """
    Prepare image for Brother QL printer.
    Resize if necessary and convert to proper format.
    Width 696 is the printable width for 62mm labels on QL-800.
    """
    try:
        img = Image.open(image_path)
        
        # Convert RGBA to RGB if necessary
        if img.mode == 'RGBA':
            background = Image.new('RGB', img.size, (255, 255, 255))
            background.paste(img, mask=img.split()[3])
            img = background
        elif img.mode != 'RGB':
            img = img.convert('RGB')
        
        # Calculate new dimensions if image is too wide
        if img.width > width:
            ratio = width / img.width
            new_height = int(img.height * ratio)
            # Use Resampling.LANCZOS for compatibility with all Pillow versions
            try:
                img = img.resize((width, new_height), Image.Resampling.LANCZOS)
            except AttributeError:
                # Fallback for older Pillow versions
                img = img.resize((width, new_height), Image.LANCZOS)
        
        # Convert to 1-bit black and white for better printing
        img = img.convert('1')
        
        # Save to temporary file
        with tempfile.NamedTemporaryFile(suffix='.png', delete=False) as tmp:
            img.save(tmp.name, 'PNG')
            return tmp.name
    except Exception as e:
        print(f"Error preparing image: {e}", file=sys.stderr)
        return None

def print_with_brother_ql(image_path, device='/dev/usb/brother_ql800'):
    """
    Print image using brother_ql command.
    """
    # First, prepare the image
    prepared_image = prepare_image(image_path)
    if not prepared_image:
        return False
    
    try:
        # Try to print with brother_ql
        cmd = [
            'brother_ql',
            '--backend', 'linux_kernel',
            '--model', 'QL-800',
            '--printer', device,
            'print', '-l', '62',
            prepared_image
        ]
        
        result = subprocess.run(cmd, capture_output=True, text=True)
        
        # Clean up temporary file
        if prepared_image != image_path:
            os.unlink(prepared_image)
        
        # Check for success
        if result.returncode == 0 or 'Total:' in result.stdout:
            print("Sticker printed successfully")
            return True
        else:
            print(f"Print error: {result.stderr}", file=sys.stderr)
            return False
            
    except Exception as e:
        print(f"Error executing brother_ql: {e}", file=sys.stderr)
        # Clean up temporary file
        if prepared_image != image_path and os.path.exists(prepared_image):
            os.unlink(prepared_image)
        return False

if __name__ == '__main__':
    if len(sys.argv) < 2:
        print("Usage: python3 print_sticker.py <image_path> [device]", file=sys.stderr)
        sys.exit(1)
    
    image_path = sys.argv[1]
    device = sys.argv[2] if len(sys.argv) > 2 else '/dev/usb/brother_ql800'
    
    if not os.path.exists(image_path):
        print(f"Error: Image file not found: {image_path}", file=sys.stderr)
        sys.exit(1)
    
    success = print_with_brother_ql(image_path, device)
    sys.exit(0 if success else 1)