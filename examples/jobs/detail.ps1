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

function Add-JobField($fields, [string]$en, [string]$zh, [string]$value) {
	if ([string]::IsNullOrWhiteSpace($value)) { return }
	$v = $value | ConvertTo-Json -Compress
	$fields.Add("{`"name`":{`"en`":`"$en`",`"zh-CN`":`"$zh`"},`"value`":$v}")
}

$items = New-Object System.Collections.Generic.List[string]
foreach ($job in @(Get-IncludedServices $services)) {
	$group = 'idle'
	if ($job.State -eq 'Running') { $group = 'running' }
	if ($job.Status -in @('warn','error')) { $group = 'attention' }
	if ($job.Value -eq 'disabled') { $group = 'disabled' }
	$fields = New-Object System.Collections.Generic.List[string]
	Add-JobField $fields 'Program' '程序' $job.Program
	Add-JobField $fields 'State' '状态' $job.State
	Add-JobField $fields 'Last exit' '上次退出' $job.ExitCode
	$idJson = $job.Id | ConvertTo-Json -Compress
	$nameJson = $job.Name | ConvertTo-Json -Compress
	$valueJson = $job.Value | ConvertTo-Json -Compress
	$actions = ''
	if ($job.Action -eq 'restart') {
		$actions = ',"actions":[{"id":"restart","label":{"en":"Restart","zh-CN":"重启"}}]'
	} elseif ($job.Action -eq 'start') {
		$actions = ',"actions":[{"id":"start","label":{"en":"Run","zh-CN":"运行"}}]'
	}
	$items.Add("{`"id`":$idJson,`"name`":$nameJson,`"status`":`"$($job.Status)`",`"group`":`"$group`",`"value`":$valueJson,`"fields`":[" + ($fields -join ',') + "]$actions}")
}

'{"presentation":"compact","groups":[{"id":"running","label":{"en":"Running","zh-CN":"运行中"}},{"id":"idle","label":{"en":"Not running","zh-CN":"未运行"}},{"id":"attention","label":{"en":"Needs attention","zh-CN":"需要处理"}},{"id":"disabled","label":{"en":"Disabled","zh-CN":"已禁用"}}],"items":[' + ($items -join ',') + ']}'
