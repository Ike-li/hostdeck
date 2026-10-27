# Prints hostdeck's detail page: every drive with free space, the one that
# holds the home folder first named so, each with an action that opens it in
# File Explorer. UTF-8 with a byte order mark, like card.ps1.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
$homeDrive = $env:USERPROFILE.Substring(0, 1)
$items = @(Get-PSDrive -PSProvider FileSystem | Where-Object { $null -ne $_.Free -and ($_.Used + $_.Free) -gt 0 } | ForEach-Object {
  $size = $_.Used + $_.Free
  $used = [math]::Round(100 * $_.Used / $size)
  $status = if ($used -ge 90) { 'error' } elseif ($used -ge 80) { 'warn' } else { 'ok' }
  $name = "$($_.Name):"
  if ($_.Name -eq $homeDrive) { $name = [ordered]@{ en = "$($_.Name): (home folder disk)"; 'zh-CN' = "$($_.Name):（主目录所在磁盘）" } }
  # The large figure is free space. Used is this drive's Used, in whole GB,
  # with the percent of Used + Free.
  [ordered]@{
    id      = $_.Root
    name    = $name
    status  = $status
    value   = "$([math]::Round($_.Free / 1GB)) GB"
    label   = [ordered]@{ en = 'free'; 'zh-CN' = '可用' }
    fields  = @(
      [ordered]@{ name = [ordered]@{ en = 'Used'; 'zh-CN' = '已用' }; value = "$([math]::Round($_.Used / 1GB)) GB · $used%" },
      [ordered]@{ name = [ordered]@{ en = 'Total'; 'zh-CN' = '总量' }; value = "$([math]::Round($size / 1GB)) GB" }
    )
    actions = @([ordered]@{ id = 'open'; label = [ordered]@{ en = 'Open in File Explorer'; 'zh-CN' = '在资源管理器中打开' } })
  }
})
[ordered]@{ items = $items } | ConvertTo-Json -Depth 6 -Compress
