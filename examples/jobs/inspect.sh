# Read-only enrichment for the jobs detail page. The card and action retain
# their existing health and permission rules; no helper executes a shown command.

jobs_text() {
 printf '{"en":"%s","zh-CN":"%s"}' "$(jobs_escape "$1")" "$(jobs_escape "$2")"
}

add_text_field() {
 piece=$(printf '{"name":{"en":"%s","zh-CN":"%s"},"value":%s}' "$1" "$2" "$(jobs_text "$3" "$4")")
 if [ -n "$fields" ]; then fields="$fields,$piece"; else fields=$piece; fi
}

jobs_plist() {
 plutil -extract "$1" raw -expect "$2" -n -o - -- "$jobs_path" 2>/dev/null
}

jobs_rule() {
 if [ -n "$rule_en" ]; then rule_en="$rule_en; $1"; rule_zh="${rule_zh}；$2";
 else rule_en=$1; rule_zh=$2; fi
}

jobs_config() {
 jobs_command=$jobs_program
 rule_en= rule_zh=
 if [ -z "$jobs_path" ]; then
  add_text_field Configuration "配置读取" 'The loaded job has no configuration path.' '已加载任务没有提供配置文件路径。'
  return
 fi
 add_field 'Configuration file' "配置文件" "$jobs_path"
 if [ ! -f "$jobs_path" ] || ! plutil -lint -s -- "$jobs_path" >/dev/null 2>&1; then
  add_text_field Configuration "配置读取" 'Could not read the task configuration.' '读不到任务配置。'
  return
 fi
 if [ -z "$jobs_stdout" ]; then jobs_stdout=$(jobs_plist StandardOutPath string) || jobs_stdout=; fi
 if [ -z "$jobs_stderr" ]; then jobs_stderr=$(jobs_plist StandardErrorPath string) || jobs_stderr=; fi
 if program=$(jobs_plist Program string); then jobs_command=$program; fi
 if count=$(jobs_plist ProgramArguments array); then
  jobs_command=
  index=0
  while [ "$index" -lt "$count" ]; do
   if argument=$(jobs_plist "ProgramArguments.$index" string && printf _); then
    argument=${argument%_}
    quoted=$(printf '%s' "$argument" | sed "s/'/'\\\\''/g")
    jobs_command="${jobs_command}${jobs_command:+ }'$quoted'"
   else
    add_text_field Configuration "配置读取" 'Could not read the configured arguments.' '读不到配置的命令参数。'
    jobs_command=$jobs_program
    break
   fi
   index=$((index + 1))
  done
 fi
 # Keep all effective declarations; a conditional KeepAlive is not unconditional.
 if keep=$(jobs_plist KeepAlive bool); then
  if [ "$keep" = true ]; then jobs_rule 'Keep alive after exit' '退出后自动拉起'; fi
 elif keep=$(plutil -extract KeepAlive json -o - -- "$jobs_path" 2>/dev/null); then
  jobs_rule "Keep alive when: $keep" "满足条件时拉起：$keep"
 fi
 if mount=$(jobs_plist StartOnMount bool) && [ "$mount" = true ]; then jobs_rule 'StartOnMount: true (when a volume mounts)' '挂载卷时运行（StartOnMount: true）'; fi
 if load=$(jobs_plist RunAtLoad bool) && [ "$load" = true ]; then jobs_rule 'Run at load (login for login agents)' '加载时启动（登录代理在登录时加载）'; fi
 if interval=$(jobs_plist StartInterval integer); then jobs_rule "Every $interval seconds" "每 $interval 秒运行"; fi
 for key in StartCalendarInterval WatchPaths QueueDirectories LaunchEvents Sockets MachServices; do
  if setting=$(plutil -extract "$key" json -o - -- "$jobs_path" 2>/dev/null); then
   jobs_rule "$key: $setting" "${key}：$setting"
  fi
 done
 if [ -z "$rule_en" ]; then rule_en='No launch rule declared'; rule_zh='没有声明启动规则'; fi
 add_text_field 'When it runs' "何时运行" "$rule_en" "$rule_zh"
}

jobs_log_file() {
 [ -n "$1" ] || return 0
 if [ "$logs_remaining" -le 0 ]; then
  add_text_field "Log reading" "日志读取" "Log preview truncated at the 512 KiB page limit." "日志预览已达到单页 512 KiB 上限，后续内容已截断。"
  return
 fi
 if [ ! -f "$1" ] || [ ! -r "$1" ]; then
  add_text_field 'Log reading' "日志读取" "Could not read log: $1" "读不到日志：$1"
  return
 fi
 if ! chunk=$(tail -c 16384 -- "$1" 2>/dev/null); then
  add_text_field 'Log reading' "日志读取" "Could not read log: $1" "读不到日志：$1"
  return
 fi
 chunk=$(printf '%s\n' "$chunk" | iconv -c -f UTF-8 -t UTF-8 2>/dev/null | tail -n 20)
 [ -n "$chunk" ] || return 0
 # Include the path and separators in the shared 512 KiB page budget.
 chunk=$(printf '%s%s\n%s\n' "${jobs_log:+
}" "$1" "$chunk")
 bytes=$(printf '%s' "$chunk" | wc -c | tr -d ' ')
 if [ "$bytes" -gt "$logs_remaining" ]; then
  chunk=$(printf '%s' "$chunk" | head -c "$logs_remaining" | iconv -c -f UTF-8 -t UTF-8 2>/dev/null || true)
  bytes=$logs_remaining
  add_text_field 'Log reading' "日志读取" 'Log preview truncated at the 512 KiB page limit.' '日志预览已达到单页 512 KiB 上限，后续内容已截断。'
 fi
 logs_remaining=$((logs_remaining - bytes))
 if [ -n "$chunk" ]; then jobs_log="$jobs_log$chunk"; fi
}
