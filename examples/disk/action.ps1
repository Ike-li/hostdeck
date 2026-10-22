# Runs an action from hostdeck's detail page: "open <drive root>".
param([string]$Action, [string]$Item)
$ErrorActionPreference = 'Stop'
switch ($Action) {
  'open' { Start-Process explorer.exe -ArgumentList $Item }
  default { [Console]::Error.WriteLine("unknown action: $Action"); exit 2 }
}
