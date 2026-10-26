# Prints hostdeck's detail page: each service whose program lives outside the
# Windows directory, with Restart on a running or paused one that accepts Stop and Run on a
# stopped one that is allowed to start. A name that is not a single token has
# no action, and neither does a disabled service.
# This file is UTF-8 with a byte order mark: Windows PowerShell reads a script
# without one in the system's code page, which would garble the Chinese below.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
. "$PSScriptRoot\jobs.ps1"

try {
	$services = @(Get-CimInstance -ClassName Win32_Service -ErrorAction Stop)
} catch {
	$msg = $_.Exception.Message
	[ordered]@{
		error = [ordered]@{ en = "Could not list Windows services. $msg"; 'zh-CN' = "读不到 Windows 服务。$msg" }
	} | ConvertTo-Json -Compress
	exit 0
}

function Add-JobField($fields, [string]$en, [string]$zh, $value) {
	if ($null -eq $value -or ($value -is [string] -and [string]::IsNullOrWhiteSpace($value))) { return }
	$v = $value | ConvertTo-Json -Compress -Depth 8
	$fields.Add("{`"name`":{`"en`":`"$en`",`"zh-CN`":`"$zh`"},`"value`":$v}")
}

# Query once; match structured event properties rather than words in messages.
$events = @()
$eventError = ''
try {
 $events = @(Get-WinEvent -FilterHashtable @{LogName='System';ProviderName='Service Control Manager'} -MaxEvents 200 -ErrorAction Stop)
} catch {
 if ($_.FullyQualifiedErrorId -notlike 'NoMatchingEventsFound*') { $eventError = $_.Exception.Message }
}
$logsRemaining = 524288
$utf8 = New-Object System.Text.UTF8Encoding $false

function Get-JobStateText($job) {
 $zh = switch ($job.Value) {
  'running' { '运行中' }
  'idle' { '未运行' }
  'disabled' { '已禁用' }
  'Paused' { '已暂停' }
  'Start Pending' { '正在启动' }
  'Stop Pending' { '正在停止' }
  'Continue Pending' { '正在继续' }
  'Pause Pending' { '正在暂停' }
  default { "未运行或状态异常 · $($job.Value)" }
 }
 return @{en=$job.Value;'zh-CN'=$zh}
}

$items = New-Object System.Collections.Generic.List[string]
foreach ($job in @(Get-IncludedServices $services)) {
	$group = 'idle'
	if ($job.State -eq 'Running') { $group = 'running' }
	if ($job.Status -in @('warn','error')) { $group = 'attention' }
	if ($job.Value -eq 'disabled') { $group = 'disabled' }
	$fields = New-Object System.Collections.Generic.List[string]
	Add-JobField $fields 'Service ID' '服务 ID' $job.Id
	if ($job.PID -and $job.PID -ne '0') { Add-JobField $fields 'PID' '进程 ID' $job.PID }
	Add-JobField $fields 'Start mode' '启动方式' @{en=$job.StartMode;'zh-CN'=$(switch ($job.StartMode) {'Auto' {'自动'} 'Manual' {'手动'} 'Disabled' {'已禁用'} default {$job.StartMode}})}
	Add-JobField $fields 'Account' '运行账户' $job.Account
	Add-JobField $fields 'Command' '完整命令' $job.Command
	Add-JobField $fields 'Program' '程序' $job.Program
	Add-JobField $fields 'State' '状态' (Get-JobStateText $job)
	Add-JobField $fields 'Last exit' '上次退出' $job.ExitCode
	$idJson = $job.Id | ConvertTo-Json -Compress
	$nameJson = $job.Name | ConvertTo-Json -Compress
	$valueJson = Get-JobStateText $job | ConvertTo-Json -Compress
 $log = ''
 if ($eventError) {
  Add-JobField $fields 'Log reading' '日志读取' @{en="Could not read service events: $eventError";'zh-CN'="读不到服务事件：$eventError"}
 } else {
  try {
   $matching = @($events | Where-Object {
    $values = @($_.Properties | ForEach-Object { [string]$_.Value })
    $values -contains $job.Id -or $values -contains $job.Name
   } | Select-Object -First 20)
   [array]::Reverse($matching)
   $log = ($matching | ForEach-Object { "$(([datetime]$_.TimeCreated).ToString('o')) [$($_.Id)] $($_.Message)" }) -join "`n"
   if (-not $log) { Add-JobField $fields 'Log reading' '日志读取' @{en='No matching events among the latest 200 service events.';'zh-CN'='最近 200 条服务事件中没有匹配记录。'} }
  } catch {
   Add-JobField $fields 'Log reading' '日志读取' @{en="Could not read service events: $($_.Exception.Message)";'zh-CN'="读不到服务事件：$($_.Exception.Message)"}
   $log = ''
  }
 }
 $bytes = $utf8.GetBytes($log)
 if ($bytes.Length -gt $logsRemaining) {
  $log = $utf8.GetString($bytes,0,$logsRemaining)
  while ($utf8.GetByteCount($log) -gt $logsRemaining) { $log = $log.Substring(0,$log.Length-1) }
  Add-JobField $fields 'Log reading' '日志读取' @{en='Log preview truncated at the 512 KiB page limit.';'zh-CN'='日志预览已达到单页 512 KiB 上限，后续内容已截断。'}
 }
 $logsRemaining -= $utf8.GetByteCount($log)
 $logJson = $log | ConvertTo-Json -Compress
	$actions = ''
	if ($job.Action -eq 'restart') {
		$actions = ',"actions":[{"id":"restart","label":{"en":"Restart","zh-CN":"重启"}}]'
	} elseif ($job.Action -eq 'start') {
		$actions = ',"actions":[{"id":"start","label":{"en":"Run","zh-CN":"运行"}}]'
	}
	$items.Add("{`"id`":$idJson,`"name`":$nameJson,`"status`":`"$($job.Status)`",`"group`":`"$group`",`"value`":$valueJson,`"log`":$logJson,`"fields`":[" + ($fields -join ',') + "]$actions}")
}

'{"presentation":"compact","groups":[{"id":"running","label":{"en":"Running","zh-CN":"运行中"}},{"id":"idle","label":{"en":"Not running","zh-CN":"未运行"}},{"id":"attention","label":{"en":"Needs attention","zh-CN":"需要处理"}},{"id":"disabled","label":{"en":"Disabled","zh-CN":"已禁用"}}],"items":[' + ($items -join ',') + ']}'
