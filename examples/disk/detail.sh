#!/bin/sh
# Prints hostdeck's detail page: the disk that holds the home folder and every
# volume mounted under /Volumes, each with an action that opens it in Finder.
set -eu
home=$(df -Pk "$HOME" | awk 'NR == 2 { m = $6; for (i = 7; i <= NF; i++) m = m " " $i; print m }')
df -Pkl | awk -v home="$home" '
BEGIN { printf "{\"items\": ["; n = 0 }
NR > 1 {
	mount = $6
	for (i = 7; i <= NF; i++) mount = mount " " $i
	if (mount != home && mount !~ /^\/Volumes\//) next
	used = $5 + 0
	status = "ok"
	if (used >= 80) status = "warn"
	if (used >= 90) status = "error"
	gsub(/\\/, "\\\\", mount)
	gsub(/"/, "\\\"", mount)
	if (mount == home) name = "{\"en\": \"Home folder disk\", \"zh-CN\": \"主目录所在磁盘\"}"
	else { name = mount; sub(/^\/Volumes\//, "", name); name = "\"" name "\"" }
	printf "%s{\"id\": \"%s\", \"name\": %s, \"status\": \"%s\", \"value\": \"%.0f GB\", ", (n++ ? ", " : ""), mount, name, status, $4 / 1048576
	printf "\"fields\": [{\"name\": {\"en\": \"Total\", \"zh-CN\": \"总量\"}, \"value\": \"%.0f GB\"}, ", $2 / 1048576
	printf "{\"name\": {\"en\": \"Used\", \"zh-CN\": \"已用\"}, \"value\": \"%d%%\"}, ", used
	printf "{\"name\": {\"en\": \"Mounted at\", \"zh-CN\": \"挂载点\"}, \"value\": \"%s\"}], ", mount
	printf "\"actions\": [{\"id\": \"open\", \"label\": {\"en\": \"Open in Finder\", \"zh-CN\": \"在访达中打开\"}}]}"
}
END { print "]}" }'
