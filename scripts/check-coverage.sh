#!/bin/sh
# Coverage gates: 85% overall with -coverpkg=./..., 90% statement coverage
# in ircx, ircstate, identity, queue, store, and bot.
# go test ./... concatenates per-package profiles; merge by max hit count.
set -e
PROFILE=${1:-coverage.out}
OVERALL_MIN=${OVERALL_MIN:-85}
CRITICAL_MIN=${CRITICAL_MIN:-90}

if [ ! -f "$PROFILE" ]; then
  echo "missing coverage profile: $PROFILE" >&2
  exit 1
fi

func=$(go tool cover -func="$PROFILE")
printf '%s\n' "$func" | tail -20

total=$(printf '%s\n' "$func" | awk '/^total:/{gsub("%","",$3); print $3; exit}')
awk -v t="$total" -v min="$OVERALL_MIN" 'BEGIN {
  if (t+0 < min+0) { printf "overall coverage %s%% is below %s%%\n", t, min; exit 1 }
  printf "overall coverage %s%% (floor %s%%)\n", t, min
}'

crit=$(awk '
  /^mode:/ { next }
  NF < 3 { next }
  {
    key=$1; stmt=$2; cnt=$3
    stmts[key]=stmt
    if (cnt+0 > hits[key]+0) hits[key]=cnt
  }
  END {
    for (k in stmts) {
      file=k
      sub(/:[0-9]+\.[0-9]+,[0-9]+\.[0-9]+$/, "", file)
      n=split(file, parts, "/")
      pkg=""
      for (i=1; i<n; i++) pkg=pkg (i>1 ? "/" : "") parts[i]
      tot[pkg]+=stmts[k]
      if (hits[k]+0 > 0) cov[pkg]+=stmts[k]
    }
    for (p in tot) printf "%s %.1f\n", p, (100*cov[p]/tot[p])
  }
' "$PROFILE")

printf '%s\n' "$crit" | sort

fail=0
for pkg in eggbot/internal/ircx eggbot/internal/ircstate eggbot/internal/identity eggbot/internal/queue eggbot/internal/store eggbot/internal/bot; do
  pt=$(printf '%s\n' "$crit" | awk -v p="$pkg" '$1==p { print $2; exit }')
  if [ -z "$pt" ]; then
    echo "no coverage rows for $pkg" >&2
    fail=1
    continue
  fi
  echo "$pkg statement coverage ${pt}%"
  awk -v t="$pt" -v min="$CRITICAL_MIN" -v p="$pkg" 'BEGIN {
    if (t+0 < min+0) { printf "%s coverage %s%% is below %s%%\n", p, t, min; exit 1 }
  }' || fail=1
done

exit $fail
