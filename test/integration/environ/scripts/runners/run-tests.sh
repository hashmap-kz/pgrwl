#!/usr/bin/env bash

results=()

while IFS= read -r -d '' filename; do
  echo "----------------------------------------------------------------------"
  echo ">> RUNNING: ${filename}"
  echo "----------------------------------------------------------------------"

  bash "${filename}"

  if [[ $? -ne 0 ]]; then
    results+=("$(printf "FAILED: %s\n" "${filename}")")
  else
    results+=("$(printf "OK    : %s\n" "${filename}")")
  fi

  echo "----------------------------------------------------------------------"
  echo ">> DONE: ${filename}"
  echo "----------------------------------------------------------------------"
  echo ""

done < <(find "/var/lib/postgresql/scripts/tests" -type f -name '*.sh' -print0 | sort -z)

echo ""
echo ">> TOTAL:-------------------------------------------------------------"
i=1
for elem in "${results[@]}"; do
  printf "%03d. %s\n" "${i}" "${elem}"
  ((i++))
done
