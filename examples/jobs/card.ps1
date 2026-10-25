# Prints hostdeck's card: how many services whose program lives outside the
# Windows directory need attention. A running service is fine. A stopped
# manual service is idle. A stopped automatic service needs attention, and a
# paused or pending one is a warning.
# This file is UTF-8 with a byte order mark: Windows PowerShell reads a script
# without one in the system's code page, which would garble the Chinese below.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false

function Get-ServiceProgram([string]$pathName) {
	$s = $pathName.Trim()
	if ($s.StartsWith('"')) {
		$end = $s.IndexOf('"', 1)
		if ($end -lt 0) { return $s.Trim('"') }
		return $s.Substring(1, $end - 1)
	}
	$space = $s.IndexOf(' ')
	if ($space -lt 0) { return $s }
	return $s.Substring(0, $space)
}

function Test-OutsideWindows([string]$pathName) {
	if ([string]::IsNullOrWhiteSpace($pathName)) { return $false }
	$program = Get-ServiceProgram $pathName
	if ([string]::IsNullOrWhiteSpace($program)) { return $false }
	$root = $env:SystemRoot
	if ([string]::IsNullOrWhiteSpace($root)) { $root = 'C:\Windows' }
	try {
		$full = [System.IO.Path]::GetFullPath($program)
		$rootFull = [System.IO.Path]::GetFullPath($root).TrimEnd('\')
	} catch {
		return $false
	}
	$cmp = [StringComparison]::OrdinalIgnoreCase
	if ($full.Equals($rootFull, $cmp)) { return $false }
	return -not $full.StartsWith($rootFull + '\', $cmp)
}

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
foreach ($svc in $services) {
	if (-not (Test-OutsideWindows $svc.PathName)) { continue }
	$status = 'ok'
	$text = ''
	switch ($svc.State) {
		'Running' { $status = 'ok' }
		'Stopped' {
			switch ($svc.StartMode) {
				'Disabled' { $status = 'ok' }
				'Manual' { $status = 'ok' }
				default { $status = 'error'; $text = "Stopped · $($svc.StartMode)" }
			}
		}
		default { $status = 'warn'; $text = [string]$svc.State }
	}
	if ($status -eq 'ok') { continue }
	if ($status -eq 'error') { $errors++ } else { $warns++ }
	$name = $svc.DisplayName
	if ([string]::IsNullOrWhiteSpace($name)) { $name = $svc.Name }
	$nameJson = $name | ConvertTo-Json -Compress
	$textJson = $text | ConvertTo-Json -Compress
	$pieces.Add("{`"name`":$nameJson,`"value`":$textJson,`"status`":`"$status`"}")
}

$cardStatus = if ($errors -gt 0) { 'error' } elseif ($warns -gt 0) { 'warn' } else { 'ok' }
$rowsJson = '[' + ($pieces -join ',') + ']'
'{"status":"' + $cardStatus + '","value":"' + $errors + '","label":{"en":"need attention","zh-CN":"需要处理"},"rows":' + $rowsJson + '}'
