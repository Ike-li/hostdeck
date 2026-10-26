#!/bin/sh
# Prints hostdeck's detail page: each loaded launchd job for this user, apart
# from Apple's agents and ordinary applications, with Restart on a running job
# and Run on any other. A name that is not a single launchd label has no action.
set -eu
. "$(dirname "$0")/launchd.sh"
. "$(dirname "$0")/inspect.sh"

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

if ! labels=$(jobs_labels "$domain"); then
	jobs_error 'Could not list launchd jobs for this user.' '读不到当前用户的 launchd 任务。'
fi
groups='[{"id":"running","label":{"en":"Running","zh-CN":"运行中"}},{"id":"idle","label":{"en":"Not running","zh-CN":"未运行"}},{"id":"attention","label":{"en":"Needs attention","zh-CN":"需要处理"}}]'
logs_remaining=524288
items=
while IFS= read -r label; do
	[ -n "$label" ] || continue
	case "$label" in
	com.apple.*|application.*) continue ;;
	esac
	jobs_read "$domain" "$label"
	jobs_classify
	group=idle
	if [ "$jobs_state" = running ]; then group=running; fi
	if [ "$jobs_status" = error ] || [ "$jobs_status" = warn ]; then group=attention; fi
	case "$group" in
	running) value_en=Running; value_zh=运行中 ;;
	idle) value_en="Not running"; value_zh=未运行 ;;
	*)
  value_en=$jobs_value
  if [ -n "$jobs_signal" ]; then value_zh="未运行 · 终止信号 $jobs_signal";
  elif [ -n "$jobs_code" ] && [ "$jobs_code" != 0 ] && [ "$jobs_code" != "(never exited)" ]; then value_zh="未运行 · 退出码 $jobs_code";
  else value_zh="状态异常 · $jobs_state"; fi ;;
	esac
	value=$(jobs_text "$value_en" "$value_zh")
	fields=
	jobs_config
	[ -n "$jobs_pid" ] && add_field PID "进程 ID" "$jobs_pid"
	[ -n "$jobs_command" ] && add_field Command "完整命令" "$jobs_command"
	[ -n "$jobs_program" ] && add_field Program "程序" "$jobs_program"
	[ -n "$jobs_state" ] && add_text_field State "状态" "$value_en" "$value_zh"
	[ -n "$jobs_code" ] && add_field "Last exit" "上次退出" "$jobs_code"
	[ -n "$jobs_signal" ] && add_field "Last signal" "上次终止信号" "$jobs_signal"
	[ -n "$jobs_runs" ] && add_field Runs "运行次数" "$jobs_runs"
	[ -n "$jobs_stdout" ] && add_field "Standard output" "标准输出" "$jobs_stdout"
	[ -n "$jobs_stderr" ] && add_field "Standard error" "标准错误" "$jobs_stderr"
	jobs_log=
	jobs_log_file "$jobs_stderr"
	if [ "$jobs_stdout" != "$jobs_stderr" ]; then jobs_log_file "$jobs_stdout"; fi
	item=$(printf '{"id":"%s","name":"%s","status":"%s","value":%s,"fields":[%s]' \
		"$(jobs_escape "$label")" "$(jobs_escape "$label")" "$jobs_status" "$value" "$fields")
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
	if [ -n "$jobs_log" ]; then item="$item,\"log\":\"$(jobs_escape "$jobs_log")\""; fi
	item="$item,\"group\":\"$group\"}"
	if [ -n "$items" ]; then
		items="$items,$item"
	else
		items=$item
	fi
done <<EOF
$labels
EOF

printf '{"presentation":"compact","groups":%s,"items":[%s]}\n' "$groups" "$items"
