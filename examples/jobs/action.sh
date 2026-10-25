#!/bin/sh
# Runs an action from hostdeck's detail page: "restart <label>" or "start <label>".
# restart is launchctl kickstart -k; start is kickstart without -k.
set -eu
. "$(dirname "$0")/launchd.sh"

action=${1:-}
label=${2:-}
case "$action" in
start|restart) ;;
*) echo "unknown action: $action" >&2; exit 2 ;;
esac

if ! jobs_safe "$label"; then
	printf '%s\n' '{"error":{"en":"This job name cannot be passed to launchctl.","zh-CN":"这条任务的名字不能传给 launchctl。"}}'
	exit 0
fi

if ! domain=$(jobs_domain); then
	printf '%s\n' '{"error":{"en":"Could not list launchd jobs for this user.","zh-CN":"读不到当前用户的 launchd 任务。"}}'
	exit 0
fi

jobs_failed() {
	last=$(printf '%s\n' "$3" | tail -n 1)
	printf '{"error":{"en":"%s: %s","zh-CN":"%s：%s"}}\n' \
		"$(jobs_escape "$1")" "$(jobs_escape "$last")" "$(jobs_escape "$2")" "$(jobs_escape "$last")"
	exit 0
}

if [ "$action" = restart ]; then
	if ! err=$(launchctl kickstart -k "$domain/$label" 2>&1); then
		jobs_failed "Could not restart the job" "没能重启这条任务" "$err"
	fi
else
	if ! err=$(launchctl kickstart "$domain/$label" 2>&1); then
		jobs_failed "Could not run the job" "没能运行这条任务" "$err"
	fi
fi
