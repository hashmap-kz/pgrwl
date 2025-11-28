#!/usr/bin/env bash

results=()

# track global exit code
exitcode=0   

# SECONDS starts from 0 when the shell starts;
# save the starting point so we can compute total later
script_start=$SECONDS

while IFS= read -r -d '' filename; do
  echo "::group::TEST ${filename}"
  echo "----------------------------------------------------------------------"
  echo ">> RUNNING: ${filename}"
  echo "----------------------------------------------------------------------"

  # remember start time for this test
  start=$SECONDS

  if bash -x "${filename}"; then
    status="OK"
  else
    status="FAILED"
     # record failure
    exitcode=1      
  fi

  # elapsed time in seconds for this test
  elapsed=$(( SECONDS - start ))

  results+=("$(printf "%-6s: (%3ds) %s" "${status}" "${elapsed}" "${filename}")")

  echo "----------------------------------------------------------------------"
  echo ">> DONE: ${filename} (time: ${elapsed}s)"
  echo "----------------------------------------------------------------------"
  echo ""
  echo "::endgroup::"

done < <(find "/var/lib/postgresql/scripts/tests" -type f -name '[0-9][0-9][0-9]-*.sh' -print0 | sort -z)

total_elapsed=$(( SECONDS - script_start ))

echo "::group::TOTAL"
echo ""
echo ">> TOTAL:-------------------------------------------------------------"
i=1
for elem in "${results[@]}"; do
  printf "%03d. %s\n" "${i}" "${elem}"
  ((i++))
done

echo ""
echo "Total time: ${total_elapsed}s"
echo "::endgroup::"

exit $exitcode   # <--- exit with collected result
