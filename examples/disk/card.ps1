# Prints hostdeck's card for the drive that holds the home folder.
# This file is UTF-8 with a byte order mark: Windows PowerShell reads a script
# without one in the system's code page, which would garble the Chinese below.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
$drive = Get-PSDrive -Name $env:USERPROFILE.Substring(0, 1)
$size = $drive.Used + $drive.Free
$used = [math]::Round(100 * $drive.Used / $size)
$status = if ($used -ge 90) { 'error' } elseif ($used -ge 80) { 'warn' } else { 'ok' }
$free = [math]::Round($drive.Free / 1GB)
$total = [math]::Round($size / 1GB)
[ordered]@{
  status = $status
  value  = "$free GB"
  label  = [ordered]@{ en = "free of $total GB"; 'zh-CN' = "可用，共 $total GB" }
  rows   = @([ordered]@{ name = [ordered]@{ en = 'Used'; 'zh-CN' = '已用' }; value = "$used%"; status = $status })
} | ConvertTo-Json -Depth 4 -Compress
