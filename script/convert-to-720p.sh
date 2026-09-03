#!/bin/bash

# Usage:
# ./convert-to-720p.sh "/path/to/video/folder"

INPUT_DIR="$1"
OUTPUT_DIR="$INPUT_DIR/converted_720p"

if [ -z "$INPUT_DIR" ]; then
  echo "Usage: $0 /path/to/video/folder"
  exit 1
fi

if [ ! -d "$INPUT_DIR" ]; then
  echo "Error: Folder does not exist: $INPUT_DIR"
  exit 1
fi

mkdir -p "$OUTPUT_DIR"

# Supported video extensions
EXTENSIONS=("mp4" "mov" "mkv" "avi" "webm" "m4v" "flv" "wmv")

for EXT in "${EXTENSIONS[@]}"; do
  while IFS= read -r -d '' FILE; do
    BASENAME=$(basename "$FILE")
    NAME="${BASENAME%.*}"
    OUTPUT_FILE="$OUTPUT_DIR/${NAME}_720p.mp4"

    echo "Converting: $BASENAME"

    if ffmpeg -y -nostdin \
      -i "$FILE" \
      -vf "scale=-2:720" \
      -c:v libx264 \
      -preset medium \
      -crf 23 \
      -c:a aac \
      -b:a 128k \
      -movflags +faststart \
      "$OUTPUT_FILE"; then
      echo "Done: $OUTPUT_FILE"
    else
      echo "Failed: $FILE"
    fi

    echo "----------------------------------------"
  done < <(find "$INPUT_DIR" -maxdepth 1 -type f -iname "*.$EXT" -print0)
done

echo "All conversions complete."
echo "Output folder: $OUTPUT_DIR"