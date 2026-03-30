#!/bin/bash

# Usage: ./ytmp3.sh "search keywords"

if [ -z "$1" ]; then
  echo "Usage: $0 \"search keywords\""
  exit 1
fi

QUERY="$1"

yt-dlp "ytsearch1:$QUERY" \
  -x --audio-format mp3 --audio-quality 192K \
  --output "%(title)s.%(ext)s"
