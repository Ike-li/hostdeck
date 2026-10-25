#!/bin/sh
# Prints hostdeck's card: how many of this user's loaded launchd jobs need
# attention. Apple's agents and ordinary applications are left out. A running
# job is fine whatever its last exit was. A job that is not running is fine
# when its last exit was 0 or it has no last exit.
set -eu
. "$(dirname "$0")/launchd.sh"

if ! domain=$(jobs_domain); then
	printf '%s\n' '{"error":{"en":"Could not list launchd jobs for this user.","zh-CN":"读不到当前用户的 launchd 任务。"}}'
	exit 0
fi

labels=$(jobs_labels "$domain")
rows=
errors=0
while IFS= read -r label; do
	[ -n "$label" ] || continue
	case "$label" in
	com.apple.*|application.*) continue ;;
	esac
	jobs_read "$domain" "$label"
	jobs_classify
	if [ "$jobs_status" != error ]; then
		continue
	fi
	errors=$((errors + 1))
	piece=$(printf '{"name":"%s","value":"%s","status":"error"}' "$(jobs_escape "$label")" "$(jobs_escape "$jobs_value")")
	if [ -n "$rows" ]; then
		rows="$rows,$piece"
	else
		rows=$piece
	fi
done <<EOF
$labels
EOF

if [ "$errors" -gt 0 ]; then
	status=error
else
	status=ok
fi
printf '{"status":"%s","value":"%s","label":{"en":"need attention","zh-CN":"需要处理"},"rows":[%s]}\n' "$status" "$errors" "$rows"
