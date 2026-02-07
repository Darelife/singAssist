#!/usr/bin/env python3
"""
Audio separator using Demucs (or Spleeter as fallback).
Separates vocals and accompaniment from an input audio file.

Usage: python separate.py <input.mp3> <output_dir>
Output: Creates vocals.mp3 and accompaniment.mp3 in output_dir
"""

import sys
import os
import subprocess
import shutil

def check_demucs():
    """Check if demucs is installed."""
    try:
        subprocess.run(["demucs", "--help"], capture_output=True, check=True)
        return True
    except (subprocess.CalledProcessError, FileNotFoundError):
        return False

def check_spleeter():
    """Check if spleeter is installed."""
    try:
        subprocess.run(["spleeter", "--help"], capture_output=True, check=True)
        return True
    except (subprocess.CalledProcessError, FileNotFoundError):
        return False

def separate_with_demucs(input_file, output_dir):
    """Separate using Demucs (better quality)."""
    print(f"Separating with Demucs: {input_file}")
    
    # Demucs outputs to separated/<model_name>/<track_name>/
    # Use --mp3 to output directly to mp3 and avoid torchcodec issues
    result = subprocess.run([
        "demucs", 
        "--two-stems=vocals",  # Only separate vocals vs rest
        "--mp3",               # Output as MP3 directly
        "--mp3-bitrate", "320",
        "-o", output_dir,
        input_file
    ], capture_output=True, text=True)
    
    if result.returncode != 0:
        print(f"Demucs error: {result.stderr}")
        return False
    
    # Find the output files
    base_name = os.path.splitext(os.path.basename(input_file))[0]
    stems_dir = os.path.join(output_dir, "htdemucs", base_name)
    
    if os.path.exists(stems_dir):
        # With --mp3 flag, demucs outputs .mp3 files directly
        vocals_src = os.path.join(stems_dir, "vocals.mp3")
        no_vocals_src = os.path.join(stems_dir, "no_vocals.mp3")
        
        # Fallback to wav if mp3 not found
        if not os.path.exists(vocals_src):
            vocals_src = os.path.join(stems_dir, "vocals.wav")
            no_vocals_src = os.path.join(stems_dir, "no_vocals.wav")
        
        # Copy/convert to expected locations
        if vocals_src.endswith(".mp3"):
            shutil.copy(vocals_src, os.path.join(output_dir, "vocals.mp3"))
            shutil.copy(no_vocals_src, os.path.join(output_dir, "accompaniment.mp3"))
        else:
            # Convert wav to mp3
            for src, dst_name in [(vocals_src, "vocals"), (no_vocals_src, "accompaniment")]:
                mp3_path = os.path.join(output_dir, f"{dst_name}.mp3")
                subprocess.run(["ffmpeg", "-i", src, "-q:a", "2", mp3_path, "-y"], 
                             capture_output=True)
        
        # Cleanup
        shutil.rmtree(os.path.join(output_dir, "htdemucs"), ignore_errors=True)
        return True
    
    return False

def separate_with_spleeter(input_file, output_dir):
    """Separate using Spleeter (faster but lower quality)."""
    print(f"Separating with Spleeter: {input_file}")
    
    result = subprocess.run([
        "spleeter", "separate",
        "-p", "spleeter:2stems",
        "-o", output_dir,
        input_file
    ], capture_output=True, text=True)
    
    if result.returncode != 0:
        print(f"Spleeter error: {result.stderr}")
        return False
    
    # Spleeter outputs to <output_dir>/<track_name>/vocals.wav and accompaniment.wav
    base_name = os.path.splitext(os.path.basename(input_file))[0]
    stems_dir = os.path.join(output_dir, base_name)
    
    if os.path.exists(stems_dir):
        for name in ["vocals", "accompaniment"]:
            src = os.path.join(stems_dir, f"{name}.wav")
            dst = os.path.join(output_dir, f"{name}.mp3")
            if os.path.exists(src):
                subprocess.run(["ffmpeg", "-i", src, "-q:a", "2", dst, "-y"],
                             capture_output=True)
        shutil.rmtree(stems_dir, ignore_errors=True)
        return True
    
    return False

def main():
    if len(sys.argv) < 3:
        print("Usage: python separate.py <input.mp3> <output_dir>")
        sys.exit(1)
    
    input_file = sys.argv[1]
    output_dir = sys.argv[2]
    
    if not os.path.exists(input_file):
        print(f"Error: Input file not found: {input_file}")
        sys.exit(1)
    
    os.makedirs(output_dir, exist_ok=True)
    
    # Check what's available
    has_demucs = check_demucs()
    has_spleeter = check_spleeter()
    
    print(f"Demucs available: {has_demucs}")
    print(f"Spleeter available: {has_spleeter}")
    
    success = False
    
    if has_demucs:
        success = separate_with_demucs(input_file, output_dir)
    elif has_spleeter:
        success = separate_with_spleeter(input_file, output_dir)
    else:
        print("Error: Neither Demucs nor Spleeter is installed.")
        print("Install with: pip install demucs  OR  pip install spleeter")
        sys.exit(1)
    
    if success:
        print(f"Separation complete! Files in: {output_dir}")
        print(f"  - vocals.mp3")
        print(f"  - accompaniment.mp3")
    else:
        print("Separation failed!")
        sys.exit(1)

if __name__ == "__main__":
    main()
