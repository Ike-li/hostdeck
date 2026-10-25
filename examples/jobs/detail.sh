#!/bin/sh
# Prints hostdeck's detail page: each loaded launchd job for this user, apart
# from Apple's agents and ordinary applications, with Restart on a running job
# and Run on any other. A name that is not a single launchd label has no action.
set -eu
. "$(dirname "$0")/launchd.sh"

if ! domain=$(jobs_domain); then
	printf '%s\n' '{"error":{"en":"Could not list launchd jobs for this user.","zh-CN":"读不到当前用户的 launchd 任务。"}}'
	exit 0
fi

add_field() {
	piece=$(printf '{"name":{"en":"%s","zh-CN":"%s"},"value":"%s"}' "$1" "$2" "$(jobs_escape "$3")")
	if [ -n "$fields" ]; then
		fields="$fields,$piece"
	else
		fields=$piece
	fi
}

labels=$(jobs_labels "$domain")
items=
while IFS= read -r label; do
	[ -n "$label" ] || continue
	case "$label" in
	com.apple.*|application.*) continue ;;
	esac
	jobs_read "$domain" "$label"
	jobs_classify
	fields=
	[ -n "$jobs_program" ] && add_field Program "程序" "$jobs_program"
	[ -n "$jobs_state" ] && add_field State "状态" "$jobs_state"
	[ -n "$jobs_code" ] && add_field "Last exit" "上次退出" "$jobs_code"
	[ -n "$jobs_runs" ] && add_field Runs "运行次数" "$jobs_runs"
	[ -n "$jobs_stdout" ] && add_field "Standard output" "标准输出" "$jobs_stdout"
	item=$(printf '{"id":"%s","name":"%s","status":"%s","value":"%s","fields":[%s]' \
		"$(jobs_escape "$label")" "$(jobs_escape "$label")" "$jobs_status" "$(jobs_escape "$jobs_value")" "$fields")
	if jobs_safe "$label"; then
		case "$jobs_state" in
		running)
			item="$item,\"actions\":[{\"id\":\"restart\",\"label\":{\"en\":\"Restart\",\"zh-CN\":\"重启\"}}]"
			;;
		*)
			item="$item,\"actions\":[{\"id\":\"start\",\"label\":{\"en\":\"Run\",\"zh-CN\":\"运行\"}}]"
			;;
		esac
	fi
	item="$item}"
	if [ -n "$items" ]; then
		items="$items,$item"
	else
		items=$item
	fi
done <<EOF
$labels
EOF

printf '{"items":[%s]}\n' "$items"
