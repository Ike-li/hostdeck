# Shared by the jobs plugin's Windows scripts. A service counts when its
# program lives outside the Windows directory.

function Test-JobName([string]$name) {
	return $name -cmatch '\A[A-Za-z0-9._-]+\z'
}

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

function Get-IncludedServices($Services) {
	foreach ($svc in @($Services)) {
		if (-not (Test-OutsideWindows ([string]$svc.PathName))) { continue }
		$status = 'ok'
		$value = ''
		$action = ''
		switch ([string]$svc.State) {
			'Running' { $value = 'running'; $action = 'restart' }
			'Stopped' {
				switch ([string]$svc.StartMode) {
					'Disabled' { $value = 'disabled' }
					'Manual' { $value = 'idle'; $action = 'start' }
					default {
						$status = 'error'
						$value = "Stopped · $([string]$svc.StartMode)"
						$action = 'start'
					}
				}
			}
			default {
				$status = 'warn'
				$value = [string]$svc.State
				$action = 'restart'
			}
		}
		if ([string]$svc.StartMode -notin @('Auto', 'Manual')) { $action = '' }
		if ($action -eq 'restart' -and ([string]$svc.State -notin @('Running', 'Paused') -or -not $svc.AcceptStop)) { $action = '' }
		$id = [string]$svc.Name
		if (-not (Test-JobName $id)) { $action = '' }
		$name = [string]$svc.DisplayName
		if ([string]::IsNullOrWhiteSpace($name)) { $name = $id }
		[pscustomobject]@{
			Id       = $id
			Name     = $name
			Status   = $status
			Value    = $value
			Action   = $action
			State    = [string]$svc.State
			Program  = Get-ServiceProgram ([string]$svc.PathName)
			ExitCode = [string]$svc.ExitCode
		}
	}
}
