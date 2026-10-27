# Prints hostdeck's detail page: each service whose program lives outside the
# Windows directory, with Restart on a running or paused one that accepts Stop and Run on a
# stopped one that is allowed to start. A name that is not a single token has
# no action, and neither does a disabled service.
# This file is UTF-8 with a byte order mark: Windows PowerShell reads a script
# without one in the system's code page, which would garble the Chinese below.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
. "$PSScriptRoot\jobs.ps1"
$only = [string]$env:HOSTDECK_DETAIL_ITEM

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
	if ($only -and $job.Id -ne $only) { continue }
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
	if ($only) {
		$events = @()
		$eventError = ''
		try {
			$events = @(Get-WinEvent -FilterHashtable @{LogName='System';ProviderName='Service Control Manager'} -MaxEvents 400 -ErrorAction Stop)
		} catch {
			if ($_.FullyQualifiedErrorId -notlike 'NoMatchingEventsFound*') { $eventError = $_.Exception.Message }
		}
		if ($eventError) {
			Add-JobField $fields 'Log reading' '日志读取' @{en="Could not read service events: $eventError";'zh-CN'="读不到服务事件：$eventError"}
		} else {
			try {
				$matching = @($events | Where-Object {
					$values = @($_.Properties | ForEach-Object { [string]$_.Value })
					$values -contains $job.Id -or $values -contains $job.Name
				} | Select-Object -First 100)
				[array]::Reverse($matching)
				$log = ($matching | ForEach-Object { "$(([datetime]$_.TimeCreated).ToString('o')) [$($_.Id)] $($_.Message)" }) -join "`n"
				if (-not $log) { Add-JobField $fields 'Log reading' '日志读取' @{en='No matching events among the latest 400 service events.';'zh-CN'='最近 400 条服务事件中没有匹配记录。'} }
				if ($utf8.GetByteCount($log) -gt 262144) {
					$bytes = $utf8.GetBytes($log)
					$start = $bytes.Length - 262144
					$slice = New-Object byte[] 262144
					[Array]::Copy($bytes, $start, $slice, 0, 262144)
					$log = $utf8.GetString($slice)
					$cut = $log.IndexOf("`n")
					if ($cut -ge 0 -and ($cut + 1) -lt $log.Length) { $log = $log.Substring($cut + 1) }
					Add-JobField $fields 'Log reading' '日志读取' @{en='Showing the last 256 KiB of this log.';'zh-CN'='只显示这段日志的末尾 256 KiB。'}
				}
			} catch {
				Add-JobField $fields 'Log reading' '日志读取' @{en="Could not read service events: $($_.Exception.Message)";'zh-CN'="读不到服务事件：$($_.Exception.Message)"}
				$log = ''
			}
		}
	}
	$logPart = ''
	if ($log) {
		$logJson = $log | ConvertTo-Json -Compress
		$logPart = ",`"log`":$logJson"
	}
	$actions = ''
	if ($job.Action -eq 'restart') {
		$actions = ',"actions":[{"id":"restart","label":{"en":"Restart","zh-CN":"重启"}}]'
	} elseif ($job.Action -eq 'start') {
		$actions = ',"actions":[{"id":"start","label":{"en":"Run","zh-CN":"运行"}}]'
	}
	$items.Add("{`"id`":$idJson,`"name`":$nameJson,`"status`":`"$($job.Status)`",`"group`":`"$group`",`"value`":$valueJson$logPart,`"logAvailable`":true,`"fields`":[" + ($fields -join ',') + "]$actions}")
}
if ($only -and $items.Count -eq 0) {
	[ordered]@{ error = [ordered]@{ en = "Could not read this service: $only"; 'zh-CN' = "读不到这个服务：$only" } } | ConvertTo-Json -Compress
	exit 0
}

'{"presentation":"compact","groups":[{"id":"running","label":{"en":"Running","zh-CN":"运行中"}},{"id":"idle","label":{"en":"Not running","zh-CN":"未运行"}},{"id":"attention","label":{"en":"Needs attention","zh-CN":"需要处理"}},{"id":"disabled","label":{"en":"Disabled","zh-CN":"已禁用"}}],"items":[' + ($items -join ',') + ']}'
