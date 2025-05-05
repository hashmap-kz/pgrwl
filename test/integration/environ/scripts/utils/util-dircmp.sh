#!/usr/bin/env bash
set -euo pipefail

DIR_A="$1"
DIR_B="$2"

declare -A filesA
declare -A filesB

# Populate filesA with relative paths and full paths
while IFS= read -r -d '' file; do
  rel="${file#$DIR_A/}"
  filesA["$rel"]="$file"
done < <(find "$DIR_A" -type f -print0)

# Populate filesB with relative paths and full paths
while IFS= read -r -d '' file; do
  rel="${file#$DIR_B/}"
  filesB["$rel"]="$file"
done < <(find "$DIR_B" -type f -print0)

# Union of keys
all_keys=()
for k in "${!filesA[@]}"; do all_keys+=("$k"); done
for k in "${!filesB[@]}"; do all_keys+=("$k"); done
unique_keys=($(printf "%s\n" "${all_keys[@]}" | sort -u))

# Compare files
for key in "${unique_keys[@]}"; do
  pathA="${filesA[$key]-}"
  pathB="${filesB[$key]-}"

  if [[ -z "$pathA" ]]; then
    echo "$key is missing in ${DIR_A}"
  elif [[ -z "$pathB" ]]; then
    echo "$key is missing in ${DIR_B}"
  else
    sumA=$(sha256sum "$pathA" | awk '{print $1}')
    sumB=$(sha256sum "$pathB" | awk '{print $1}')
    if [[ "$sumA" != "$sumB" ]]; then
      echo "$key differs"
    fi
  fi
done
