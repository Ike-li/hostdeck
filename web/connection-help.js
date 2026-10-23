// Shell examples use literal quoting because the runtime data path is user chosen.
const sh = (value) => `'${value.replaceAll("'", "'\\''")}'`;
const ps = (value) => `'${value.replaceAll("'", "''")}'`;
const block = (shell, command) => `\n\`\`\`${shell}\n${command}\n\`\`\`\n`;

export const examplePlugin = `schema: 1
name: My service
card:
  http:
    url: http://127.0.0.1:8080/health
  value: status
  label: Status`;

export function connectionPrompts(t, pluginDir, origin) {
  const context = t('help.agent.context', { origin, pluginDir });
  const finish = t('help.agent.finish');
  const body = JSON.stringify({ address: 'http://127.0.0.1:3000/', name: 'My service', mode: 'proxy' });
  const pageSH = `curl --fail --silent --show-error ${sh(origin + '/api/pages')} \\
  -H ${sh('Origin: ' + origin)} -H 'Content-Type: application/json' \\
  --data-raw ${sh(body)}
curl --fail --silent --show-error ${sh(origin + '/api/catalog')}`;
  const pagePS = `$ErrorActionPreference = 'Stop'
Invoke-RestMethod -Method Post -Uri ${ps(origin + '/api/pages')} -Headers @{ Origin = ${ps(origin)} } -ContentType 'application/json' -Body ${ps(body)}
Invoke-RestMethod -Uri ${ps(origin + '/api/catalog')} | ConvertTo-Json -Depth 8`;
  const pluginSH = `set -eu
plugin_root=${sh(pluginDir)}
plugin_id='my-service'
if [ -e "$plugin_root/.$plugin_id" ] || [ -L "$plugin_root/.$plugin_id" ]; then
  echo 'A disabled plugin already owns this ID' >&2
  exit 1
fi
mkdir "$plugin_root/$plugin_id"
cat > "$plugin_root/$plugin_id/plugin.yaml" <<'HOSTDECK_MANIFEST'
${examplePlugin}
HOSTDECK_MANIFEST`;
  const pluginPS = `$ErrorActionPreference = 'Stop'
$pluginRoot = ${ps(pluginDir)}
$pluginId = 'my-service'
if ((Test-Path -LiteralPath (Join-Path $pluginRoot $pluginId)) -or (Test-Path -LiteralPath (Join-Path $pluginRoot ('.' + $pluginId)))) {
  throw 'An enabled or disabled plugin already owns this ID'
}
$folder = New-Item -ItemType Directory -Path (Join-Path $pluginRoot $pluginId) -ErrorAction Stop
$manifest = @'
${examplePlugin}
'@
[System.IO.File]::WriteAllText((Join-Path $folder.FullName 'plugin.yaml'), $manifest, [System.Text.UTF8Encoding]::new($false))`;
  return {
    page: [context, t('help.agent.pages'), block('sh', pageSH), block('powershell', pagePS), finish].join('\n\n'),
    plugin: [context, t('help.agent.plugins'), block('sh', pluginSH), block('powershell', pluginPS), t('help.agent.contract'), finish].join('\n\n'),
  };
}
