# Runs an action from hostdeck's detail page: "restart <name>" or "start <name>".
# restart stops the service, waits until it is stopped, then starts it.
# This file is UTF-8 with a byte order mark: Windows PowerShell reads a script
# without one in the system's code page, which would garble the Chinese below.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false

function Write-JobError([string]$en, [string]$zh) {
	$json = [ordered]@{ error = [ordered]@{ en = $en; 'zh-CN' = $zh } } | ConvertTo-Json -Compress
	# Errors must bypass Invoke-Sc's success-output suppression.
	[Console]::Out.WriteLine($json)
	exit 0
}

$action = ''
$name = ''
if ($args.Count -ge 1) { $action = [string]$args[0] }
if ($args.Count -ge 2) { $name = [string]$args[1] }
if ($action -ne 'start' -and $action -ne 'restart') {
	Write-Error "unknown action: $action"
	exit 2
}
. "$PSScriptRoot\jobs.ps1"
if (-not (Test-JobName $name)) {
	Write-JobError 'This service name cannot be passed to sc.exe.' '这个服务名不能传给 sc.exe。'
}

function Invoke-Sc([string[]]$ScArgs) {
	$log = Join-Path $env:TEMP ("hostdeck-jobs-" + [guid]::NewGuid().ToString() + '.txt')
	& sc.exe @ScArgs > $log 2>&1
	$code = $LASTEXITCODE
	$text = ''
	if (Test-Path $log) {
		$text = [IO.File]::ReadAllText($log)
		Remove-Item $log -ErrorAction SilentlyContinue
	}
	if ($code -ne 0) {
		$last = @($text -split "`r?`n" | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }) | Select-Object -Last 1
		if (-not $last) { $last = "sc.exe exited $code" }
		if ($action -eq 'start') {
			Write-JobError "Could not start the service: $last" "没能启动这个服务：$last"
		}
		Write-JobError "Could not restart the service: $last" "没能重启这个服务：$last"
	}
	return $text
}

if ($action -eq 'restart') {
	Invoke-Sc @('stop', $name) | Out-Null
	$deadline = (Get-Date).AddSeconds(15)
	do {
		$query = Invoke-Sc @('query', $name)
		if ($query -match 'STOPPED') { break }
		if ((Get-Date) -gt $deadline) {
			Write-JobError 'The service did not stop within 15 seconds.' '这个服务在 15 秒内没有停下来。'
		}
		Start-Sleep -Milliseconds 200
	} while ($true)
	Invoke-Sc @('start', $name) | Out-Null
	$leadEn = 'Restarted. '
	$leadZh = '已重启。'
	$failEn = 'Restarted, but the service could not be read again.'
	$failZh = '已重启，但没能再读到这个服务。'
} else {
	Invoke-Sc @('start', $name) | Out-Null
	$leadEn = 'Ran. '
	$leadZh = '已运行。'
	$failEn = 'Ran, but the service could not be read again.'
	$failZh = '已运行，但没能再读到这个服务。'
}

# A failed follow-up query must not hide the start or restart that succeeded.
$outcomeLog = Join-Path $env:TEMP ("hostdeck-jobs-" + [guid]::NewGuid().ToString() + '.txt')
& sc.exe query $name > $outcomeLog 2>&1
$outcomeCode = $LASTEXITCODE
$outcome = ''
if (Test-Path $outcomeLog) {
	$outcome = [IO.File]::ReadAllText($outcomeLog)
	Remove-Item $outcomeLog -ErrorAction SilentlyContinue
}
if ($outcomeCode -ne 0) {
	$en = $failEn
	$zh = $failZh
} elseif ($outcome -match 'RUNNING') {
	$en = $leadEn + 'Running.'
	$zh = $leadZh + '正在运行。'
} else {
	$en = $leadEn + 'Not running.'
	$zh = $leadZh + '未在运行。'
}
[ordered]@{ message = [ordered]@{ en = $en; 'zh-CN' = $zh } } | ConvertTo-Json -Compress
