#!/bin/sh
# Prints hostdeck's card: the free space on the disk that holds the home
# folder, and a row for it and for each volume mounted under /Volumes. Volumes
# of one APFS container share their free space, so the rows do not add up.
set -eu
home=$(df -Pk "$HOME" | awk 'NR == 2 { m = $6; for (i = 7; i <= NF; i++) m = m " " $i; print m }')
name=$(diskutil info "$home" | sed -n 's/^ *Volume Name: *//p' | sed 's/ - Data$//')
df -Pkl | awk -v home="$home" -v name="$name" '
function status(used) { return used >= 90 ? "error" : used >= 80 ? "warn" : "ok" }
function rank(s) { return s == "error" ? 2 : s == "warn" ? 1 : 0 }
function row(label, avail, used,   s) {
	gsub(/\\/, "\\\\", label)
	gsub(/"/, "\\\"", label)
	s = status(used)
	if (rank(s) > rank(worst)) worst = s
	return sprintf("{\"name\": \"%s\", \"value\": \"%.0f GB · %d%%\", \"status\": \"%s\"}", label, avail / 1048576, used, s)
}
BEGIN { worst = "ok"; others = "" }
NR > 1 {
	mount = $6
	for (i = 7; i <= NF; i++) mount = mount " " $i
	if (mount == home) {
		first = row(name, $4, $5 + 0)
		free = $4 / 1048576
		total = $2 / 1048576
		next
	}
	if (mount !~ /^\/Volumes\/[^\/]+$/ || mount == "/Volumes/Recovery") next
	label = mount
	sub(/^\/Volumes\//, "", label)
	others = others ", " row(label, $4, $5 + 0)
}
END {
	printf "{\"status\": \"%s\", \"value\": \"%.0f GB\", ", worst, free
	printf "\"label\": {\"en\": \"free of %.0f GB\", \"zh-CN\": \"可用，共 %.0f GB\"}, ", total, total
	printf "\"rows\": [%s%s]}\n", first, others
}'
