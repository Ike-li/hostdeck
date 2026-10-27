#!/bin/sh
# Prints hostdeck's detail page: each loaded launchd job for this user, apart
# from Apple's agents and ordinary applications, with Restart on a running job
# and Run on any other. A name that is not a single launchd label has no action.
# The list carries no log text. HOSTDECK_DETAIL_ITEM names one listed job and
# adds that job's log tail. The id is data: only a label from this listing is read.
set -eu
. "$(dirname "$0")/launchd.sh"
. "$(dirname "$0")/inspect.sh"

if ! jobs_open; then
	printf '%s\n' '{"error":{"en":"Could not list launchd jobs for this user.","zh-CN":"读不到当前用户的 launchd 任务。"}}'
	exit 0
fi
domain=$jobs_domain_name

if ! labels=$(printf '%s\n' "$jobs_listing" | jobs_parse_labels); then
	jobs_error 'Could not list launchd jobs for this user.' '读不到当前用户的 launchd 任务。'
fi

add_field() {
	piece=$(printf '{"name":{"en":"%s","zh-CN":"%s"},"value":"%s"}' "$1" "$2" "$(jobs_escape "$3")")
	if [ -n "$fields" ]; then
		fields="$fields,$piece"
	else
		fields=$piece
	fi
}

# jobs_write_item prints one item, or FAIL when that job cannot be read.
# It must not call jobs_error: that exits the shell, and a background worker
# would then exit the wrong process. The parent turns FAIL into the page error.
jobs_write_item() {
	label=$1
	domain=$2
	with_log=$3
	fields=
	jobs_log=
	if ! info=$(launchctl print "$domain/$label" 2>/dev/null); then
		printf 'FAIL\n'
		return 0
	fi
	jobs_apply "$info"
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
	if [ "$with_log" = log ]; then
		jobs_log_file "$jobs_stderr"
		if [ "$jobs_stdout" != "$jobs_stderr" ]; then jobs_log_file "$jobs_stdout"; fi
	fi
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
	if [ -n "$jobs_stdout$jobs_stderr" ]; then
		item="$item,\"logAvailable\":true"
	fi
	if [ -n "$jobs_log" ]; then
		item="$item,\"log\":\"$(jobs_escape "$jobs_log")\""
	fi
	item="$item,\"group\":\"$group\"}"
	printf '%s\n' "$item"
}

groups='[{"id":"running","label":{"en":"Running","zh-CN":"运行中"}},{"id":"idle","label":{"en":"Not running","zh-CN":"未运行"}},{"id":"attention","label":{"en":"Needs attention","zh-CN":"需要处理"}}]'

want=${HOSTDECK_DETAIL_ITEM-}
if [ -n "$want" ]; then
	found=
	while IFS= read -r label; do
		[ "$label" = "$want" ] || continue
		found=1
		break
	done <<EOF
$labels
EOF
	case "$want" in
	com.apple.*|application.*) found= ;;
	esac
	if [ -z "$found" ]; then
		jobs_error "Could not read launchd job: $want" "读不到这条 launchd 任务：$want"
	fi
	item=$(jobs_write_item "$want" "$domain" log)
	case "$item" in
	'{'*) ;;
	*) jobs_error "Could not read launchd job: $want" "读不到这条 launchd 任务：$want" ;;
	esac
	printf '{"presentation":"compact","groups":%s,"items":[%s]}\n' "$groups" "$item"
	exit 0
fi

# Numbered files, not the label: a label may contain a slash.
# macOS /bin/sh is bash 3.2 and has no wait -n, so each wave is four jobs.
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
i=0
while IFS= read -r label; do
	[ -n "$label" ] || continue
	case "$label" in
	com.apple.*|application.*) continue ;;
	esac
	i=$((i + 1))
	printf '%s\n' "$label" > "$work/name.$i"
done <<EOF
$labels
EOF
count=$i
start=1
while [ "$start" -le "$count" ]; do
	end=$((start + 3))
	if [ "$end" -gt "$count" ]; then end=$count; fi
	n=$start
	while [ "$n" -le "$end" ]; do
		label=$(cat "$work/name.$n")
		jobs_write_item "$label" "$domain" "" > "$work/out.$n" &
		n=$((n + 1))
	done
	wait || true
	start=$((end + 1))
done

items=
n=1
while [ "$n" -le "$count" ]; do
	label=$(cat "$work/name.$n")
	if [ ! -f "$work/out.$n" ]; then
		jobs_error "Could not read launchd job: $label" "读不到这条 launchd 任务：$label"
	fi
	item=$(cat "$work/out.$n")
	case "$item" in
	'{'*) ;;
	*) jobs_error "Could not read launchd job: $label" "读不到这条 launchd 任务：$label" ;;
	esac
	if [ -n "$items" ]; then
		items="$items,$item"
	else
		items=$item
	fi
	n=$((n + 1))
done

printf '{"presentation":"compact","groups":%s,"items":[%s]}\n' "$groups" "$items"
