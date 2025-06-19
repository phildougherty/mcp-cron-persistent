#!/bin/bash
# find_code.sh - Find and output code files for sharing with LLMs

# Define directory to search (default to current directory if not provided)
DIR="${1:-.}"

# Define file extensions to include
EXTENSIONS=("go" "proto" "yaml" "yml" "json" "md" "Dockerfile" "Makefile")

# Build the find command with extensions
FIND_ARGS=()
for ext in "${EXTENSIONS[@]}"; do
    FIND_ARGS+=(-name "*.$ext" -o)
done
# Remove the last "-o"
unset 'FIND_ARGS[${#FIND_ARGS[@]}-1]'

# Find files and process each one
find "$DIR" -type f \( "${FIND_ARGS[@]}" \) -not -path "*/\.*" -not -path "*/vendor/*" -not -path "*/node_modules/*" | sort | while read -r file; do
    # Get relative path
    rel_path=$(realpath --relative-to="$DIR" "$file")
    
    # Print file path with Markdown code block markers
    echo -e "\n## File: $rel_path\n\`\`\`${file##*.}"
    cat "$file"
    echo -e "\`\`\`\n"
done
