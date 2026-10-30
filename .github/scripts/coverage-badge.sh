#!/bin/sh -e
# Render a shields-style coverage badge SVG without third-party services.
# Usage: coverage-badge.sh <percent> <output.svg>

pct=$1
out=$2

# Integer part for colour thresholds
int=${pct%.*}
if [ "$int" -ge 90 ]; then color="#4c1"
elif [ "$int" -ge 80 ]; then color="#97ca00"
elif [ "$int" -ge 70 ]; then color="#a4a61d"
elif [ "$int" -ge 60 ]; then color="#dfb317"
elif [ "$int" -ge 50 ]; then color="#fe7d37"
else color="#e05d44"
fi

cat >"$out" <<EOF
<svg xmlns="http://www.w3.org/2000/svg" width="104" height="20" role="img" aria-label="coverage: ${pct}%">
  <title>coverage: ${pct}%</title>
  <linearGradient id="s" x2="0" y2="100%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient>
  <clipPath id="r"><rect width="104" height="20" rx="3" fill="#fff"/></clipPath>
  <g clip-path="url(#r)">
    <rect width="61" height="20" fill="#555"/>
    <rect x="61" width="43" height="20" fill="${color}"/>
    <rect width="104" height="20" fill="url(#s)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="11">
    <text x="30.5" y="15" fill="#010101" fill-opacity=".3">coverage</text>
    <text x="30.5" y="14">coverage</text>
    <text x="82.5" y="15" fill="#010101" fill-opacity=".3">${pct}%</text>
    <text x="82.5" y="14">${pct}%</text>
  </g>
</svg>
EOF
