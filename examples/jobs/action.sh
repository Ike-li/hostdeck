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
	lead_en="Restarted. "
	lead_zh="已重启。"
	fail_en="Restarted, but the job could not be read again."
	fail_zh="已重启，但没能再读到这条任务。"
else
	if ! err=$(launchctl kickstart "$domain/$label" 2>&1); then
		jobs_failed "Could not run the job" "没能运行这条任务" "$err"
	fi
	lead_en="Ran. "
	lead_zh="已运行。"
	fail_en="Ran, but the job could not be read again."
	fail_zh="已运行，但没能再读到这条任务。"
fi

# One more print says what the job is doing now. A failed print does not hide
# the kickstart that already succeeded.
if ! info=$(launchctl print "$domain/$label" 2>/dev/null); then
	en=$fail_en
	zh=$fail_zh
else
	jobs_apply "$info"
	en=$lead_en
	zh=$lead_zh
	if [ "$jobs_state" = running ]; then
		if [ -n "$jobs_pid" ]; then
			en="${en}Running, process ID ${jobs_pid}."
			zh="${zh}正在运行，进程 ID ${jobs_pid}。"
		else
			en="${en}Running."
			zh="${zh}正在运行。"
		fi
	elif [ -n "$jobs_signal" ]; then
		en="${en}Not running, signal ${jobs_signal}."
		zh="${zh}未在运行，终止信号 ${jobs_signal}。"
	elif [ -n "$jobs_code" ] && [ "$jobs_code" != 0 ] && [ "$jobs_code" != "(never exited)" ]; then
		en="${en}Not running, exit ${jobs_code}."
		zh="${zh}未在运行，退出码 ${jobs_code}。"
	else
		en="${en}Not running."
		zh="${zh}未在运行。"
	fi
fi
printf '{"message":{"en":"%s","zh-CN":"%s"}}\n' "$(jobs_escape "$en")" "$(jobs_escape "$zh")"
