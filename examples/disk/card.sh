#!/bin/sh
# Prints hostdeck's card for the disk that holds the home folder.
set -eu
df -Pk "$HOME" | awk 'NR == 2 {
	total = $2 / 1048576
	free = $4 / 1048576
	used = $5 + 0
	status = "ok"
	if (used >= 80) status = "warn"
	if (used >= 90) status = "error"
	printf "{\"status\": \"%s\", \"value\": \"%.0f GB\", ", status, free
	printf "\"label\": {\"en\": \"free of %.0f GB\", \"zh-CN\": \"可用，共 %.0f GB\"}, ", total, total
	printf "\"rows\": [{\"name\": {\"en\": \"Used\", \"zh-CN\": \"已用\"}, \"value\": \"%d%%\", \"status\": \"%s\"}]}\n", used, status
}'
