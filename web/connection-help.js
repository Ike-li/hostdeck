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

export function connectionPrompts(t, pluginDir, origin, executable) {
  const context = t('help.agent.context', { origin, pluginDir, executable });
  const finish = t('help.agent.finish');
  const pageSH = `set -eu
${sh(executable)} page add --url ${sh(origin)} --mode proxy --address 'http://127.0.0.1:3000/' --name 'My service'
${sh(executable)} plugin list --url ${sh(origin)}`;
  const pagePS = `$ErrorActionPreference = 'Stop'
& ${ps(executable)} page add --url ${ps(origin)} --mode proxy --address 'http://127.0.0.1:3000/' --name 'My service'
if ($LASTEXITCODE -ne 0) { throw 'hostdeck page add failed' }
& ${ps(executable)} plugin list --url ${ps(origin)}
if ($LASTEXITCODE -ne 0) { throw 'hostdeck plugin list failed' }`;
  const pluginSH = `set -eu
plugin_source=$(mktemp -d)
trap 'rm -rf "$plugin_source"' EXIT
cat > "$plugin_source/plugin.yaml" <<'HOSTDECK_MANIFEST'
${examplePlugin}
HOSTDECK_MANIFEST
${sh(executable)} plugin validate --from "$plugin_source"
${sh(executable)} plugin install --url ${sh(origin)} --id 'my-service' --from "$plugin_source"`;
  const pluginPS = `$ErrorActionPreference = 'Stop'
$source = Join-Path ([System.IO.Path]::GetTempPath()) ('hostdeck-plugin-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $source -ErrorAction Stop | Out-Null
try {
  $manifest = @'
${examplePlugin}
'@
  [System.IO.File]::WriteAllText((Join-Path $source 'plugin.yaml'), $manifest, [System.Text.UTF8Encoding]::new($false))
  & ${ps(executable)} plugin validate --from $source
  if ($LASTEXITCODE -ne 0) { throw 'hostdeck plugin validate failed' }
  & ${ps(executable)} plugin install --url ${ps(origin)} --id 'my-service' --from $source
  if ($LASTEXITCODE -ne 0) { throw 'hostdeck plugin install failed' }
} finally {
  Remove-Item -LiteralPath $source -Recurse -Force
}`;
  return {
    page: [context, t('help.agent.pages'), block('sh', pageSH), block('powershell', pagePS), finish].join('\n\n'),
    plugin: [context, t('help.agent.plugins'), block('sh', pluginSH), block('powershell', pluginPS), t('help.agent.contract'), finish].join('\n\n'),
  };
}
