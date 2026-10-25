#!/bin/sh
# Prints hostdeck's card: how many of this user's loaded launchd jobs need
# attention. Apple's agents and ordinary applications are left out. A running
# job is fine whatever its last exit was. A job that is not running is fine
# when its last exit was 0 or it has no last exit.
set -eu

uid=$(id -u)
if launchctl print "gui/$uid" >/dev/null 2>&1; then
	domain="gui/$uid"
elif launchctl print "user/$uid" >/dev/null 2>&1; then
	domain="user/$uid"
else
	printf '%s\n' '{"error":{"en":"Could not list launchd jobs for this user.","zh-CN":"读不到当前用户的 launchd 任务。"}}'
	exit 0
fi

labels=$(launchctl print "$domain" | awk '
	/services = \{/ { inside = 1; next }
	inside && /^[[:space:]]*\}/ { exit }
	inside && NF >= 1 { print $NF }
')

escape() {
	printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

rows=
errors=0
while IFS= read -r label; do
	[ -n "$label" ] || continue
	case "$label" in
	com.apple.*|application.*) continue ;;
	esac
	info=$(launchctl print "$domain/$label" 2>/dev/null || true)
	state=$(printf '%s\n' "$info" | sed -n 's/^[[:space:]]*state = //p' | head -n 1)
	code=$(printf '%s\n' "$info" | sed -n 's/^[[:space:]]*last exit code = //p' | head -n 1)
	case "$state" in
	running) continue ;;
	"not running")
		if [ -z "$code" ] || [ "$code" = 0 ]; then
			continue
		fi
		value="not running · exit $code"
		;;
	*)
		if [ -z "$state" ]; then
			state=unknown
		fi
		value=$state
		if [ -n "$code" ] && [ "$code" != 0 ]; then
			value="$state · exit $code"
		fi
		;;
	esac
	errors=$((errors + 1))
	piece=$(printf '{"name":"%s","value":"%s","status":"error"}' "$(escape "$label")" "$(escape "$value")")
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
