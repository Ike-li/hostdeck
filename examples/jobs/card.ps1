# Prints hostdeck's card: how many services whose program lives outside the
# Windows directory need attention. A running service is fine. A stopped
# manual service is idle. A stopped automatic service needs attention, and a
# paused or pending one is a warning.
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

$errors = 0
$warns = 0
$pieces = New-Object System.Collections.Generic.List[string]
foreach ($job in @(Get-IncludedServices $services)) {
	if ($job.Status -eq 'ok') { continue }
	if ($job.Status -eq 'error') { $errors++ } else { $warns++ }
	$nameJson = $job.Name | ConvertTo-Json -Compress
	$textJson = $job.Value | ConvertTo-Json -Compress
	$pieces.Add("{`"name`":$nameJson,`"value`":$textJson,`"status`":`"$($job.Status)`"}")
}

$cardStatus = if ($errors -gt 0) { 'error' } elseif ($warns -gt 0) { 'warn' } else { 'ok' }
$rowsJson = '[' + ($pieces -join ',') + ']'
'{"status":"' + $cardStatus + '","value":"' + ($errors + $warns) + '","label":{"en":"need attention","zh-CN":"需要处理"},"rows":' + $rowsJson + '}'
