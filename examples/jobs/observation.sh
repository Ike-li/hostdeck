#!/bin/sh
# Private, read-only source discovery for the verified built-in jobs plugin.
# Unlike detail.sh this never reads log bodies, arguments or launch rules.
# Arguments are a listed job label and an optional numeric PID filter.
set -eu
. "$(dirname "$0")/launchd.sh"
. "$(dirname "$0")/inspect.sh"
want=${1-}
pid=${2-}
if ! jobs_open; then
 jobs_error 'Could not list launchd jobs for this user.' '读不到当前用户的 launchd 任务。'
fi
if ! labels=$(printf '%s\n' "$jobs_listing" | awk -v pid="$pid" '
 /services = \{/ { inside=1; next }
 inside && /^[[:space:]]*\}/ {closed=1;exit}
 inside && NF>=1 {if(pid=="" || $1==pid) print $NF}
 END {if(!closed) exit 1}
'); then
 jobs_error 'Could not list launchd jobs for this user.' '读不到当前用户的 launchd 任务。'
fi
add_field() {
 piece=$(printf '{"name":{"en":"%s","zh-CN":"%s"},"value":"%s"}' "$1" "$2" "$(jobs_escape "$3")")
 fields="${fields}${fields:+,}$piece"
}
items=
while IFS= read -r label; do
 [ -n "$label" ] || continue
 case "$label" in com.apple.*|application.*) continue;; esac
 [ -z "$want" ] || [ "$label" = "$want" ] || continue
 jobs_read "$jobs_domain_name" "$label"
 fields=
 # launchd normally supplies the effective paths. Fall back to just the two
 # plist keys when either is absent; optional failure remains a local note.
 if [ -z "$jobs_stdout" ] || [ -z "$jobs_stderr" ]; then
  if [ -z "$jobs_path" ]; then
   add_text_field Configuration '配置读取' 'The loaded job has no configuration path.' '已加载任务没有提供配置文件路径。'
  elif [ ! -f "$jobs_path" ] || ! plutil -lint -s -- "$jobs_path" >/dev/null 2>&1; then
   add_text_field Configuration '配置读取' 'Could not read the task configuration.' '读不到任务配置。'
  else
   if [ -z "$jobs_stdout" ]; then jobs_stdout=$(jobs_plist StandardOutPath string) || jobs_stdout=; fi
   if [ -z "$jobs_stderr" ]; then jobs_stderr=$(jobs_plist StandardErrorPath string) || jobs_stderr=; fi
  fi
 fi
 [ -z "$jobs_pid" ] || add_field PID '进程 ID' "$jobs_pid"
 [ -z "$jobs_state" ] || add_field State '状态' "$jobs_state"
 [ -z "$jobs_code" ] || add_field 'Last exit' '上次退出' "$jobs_code"
 [ -z "$jobs_signal" ] || add_field 'Last signal' '上次终止信号' "$jobs_signal"
 [ -z "$jobs_runs" ] || add_field Runs '运行次数' "$jobs_runs"
 [ -z "$jobs_stdout" ] || add_field 'Standard output' '标准输出' "$jobs_stdout"
 [ -z "$jobs_stderr" ] || add_field 'Standard error' '标准错误' "$jobs_stderr"
 item=$(printf '{"id":"%s","name":"%s","fields":[%s]}' "$(jobs_escape "$label")" "$(jobs_escape "$label")" "$fields")
 items="${items}${items:+,}$item"
done <<EOF_LABELS
$labels
EOF_LABELS
printf '{"items":[%s]}\n' "$items"
