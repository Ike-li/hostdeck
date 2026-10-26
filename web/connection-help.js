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

export const exampleTasks = "{\n  \"version\": 1,\n  \"tasks\": [\n    {\n      \"id\": \"daily-backup\",\n      \"name\": \"Daily backup\",\n      \"enabled\": false,\n      \"schedule\": {\"cron\": \"0 2 * * *\", \"timezone\": \"Asia/Shanghai\"},\n      \"exec\": {\n        \"command\": [\"python3\", \"scripts/backup.py\"],\n        \"windows\": [\"py\", \"-3\", \"scripts/backup.py\"],\n        \"dir\": \".\",\n        \"env\": {\"BACKUP_TOKEN\": \"${env:BACKUP_TOKEN}\"}\n      },\n      \"timeout\": \"2h\"\n    },\n    {\n      \"id\": \"collect-local-data\",\n      \"name\": \"Collect local data\",\n      \"enabled\": false,\n      \"schedule\": {\"every\": \"30m\"},\n      \"exec\": {\"command\": [\"node\", \"scripts/collect.mjs\"]},\n      \"timeout\": \"5m\"\n    }\n  ]\n}";

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
  const taskDir = `${pluginDir}${pluginDir.includes('\\') ? '\\' : '/'}scheduled-tasks`;
  const taskSH = `set -eu
${sh(executable)} plugin install --url ${sh(origin)} --id scheduled-tasks
task_config=${sh(taskDir + '/tasks.json')}
task_backup="$(mktemp "$task_config.backup-XXXXXXXX")"
cp "$task_config" "$task_backup"
cp ${sh(taskDir + '/tasks.example.json')} "$task_config"
# Edit the disabled examples locally before enabling requested tasks.
${sh(executable)} plugin reload --url ${sh(origin)}
${sh(executable)} plugin list --url ${sh(origin)}`;
  const taskPS = `$ErrorActionPreference = 'Stop'
& ${ps(executable)} plugin install --url ${ps(origin)} --id scheduled-tasks
if ($LASTEXITCODE -ne 0) { throw 'hostdeck plugin install failed' }
$taskConfig = ${ps(taskDir + '/tasks.json')}
$taskBackup = $taskConfig + '.backup-' + [guid]::NewGuid().ToString('N')
Copy-Item -LiteralPath $taskConfig -Destination $taskBackup -ErrorAction Stop
Copy-Item -LiteralPath ${ps(taskDir + '/tasks.example.json')} -Destination $taskConfig -ErrorAction Stop
# Edit the disabled examples locally before enabling requested tasks.
& ${ps(executable)} plugin reload --url ${ps(origin)}
if ($LASTEXITCODE -ne 0) { throw 'hostdeck task reload failed' }
& ${ps(executable)} plugin list --url ${ps(origin)}
if ($LASTEXITCODE -ne 0) { throw 'hostdeck plugin list failed' }`;
  return {
    page: [context, t('help.agent.pages'), block('sh', pageSH), block('powershell', pagePS), finish].join('\n\n'),
    plugin: [context, t('help.agent.plugins'), block('sh', pluginSH), block('powershell', pluginPS), t('help.agent.contract'), t('help.agent.tasks'), block('sh', taskSH), block('powershell', taskPS), block('json', exampleTasks), finish].join('\n\n'),
  };
}
