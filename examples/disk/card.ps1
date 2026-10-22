# Prints hostdeck's card: the free space on the drive that holds the home
# folder, and a row for it and for each other drive with free space.
# This file is UTF-8 with a byte order mark: Windows PowerShell reads a script
# without one in the system's code page, which would garble the Chinese below.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
$homeDrive = $env:USERPROFILE.Substring(0, 1)
$drives = @(Get-PSDrive -PSProvider FileSystem | Where-Object { $null -ne $_.Free -and ($_.Used + $_.Free) -gt 0 } |
  Sort-Object { $_.Name -ne $homeDrive }, Name)
$rank = @{ ok = 0; warn = 1; error = 2 }
$worst = 'ok'
$rows = @($drives | ForEach-Object {
  $used = [math]::Round(100 * $_.Used / ($_.Used + $_.Free))
  $status = if ($used -ge 90) { 'error' } elseif ($used -ge 80) { 'warn' } else { 'ok' }
  if ($rank[$status] -gt $rank[$worst]) { $worst = $status }
  [ordered]@{ name = "$($_.Name):"; value = "$([math]::Round($_.Free / 1GB)) GB · $used%"; status = $status }
})
$drive = $drives[0]
$free = [math]::Round($drive.Free / 1GB)
$total = [math]::Round(($drive.Used + $drive.Free) / 1GB)
[ordered]@{
  status = $worst
  value  = "$free GB"
  label  = [ordered]@{ en = "free of $total GB"; 'zh-CN' = "可用，共 $total GB" }
  rows   = $rows
} | ConvertTo-Json -Depth 4 -Compress
