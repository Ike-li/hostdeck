# hostdeck

给你自己这台电脑用的小控制台，支持 macOS 和 Windows。本机在跑的服务、端口、容器和后台任务都在这里看、在这里管；它们的网页界面在一个地方整页打开；有什么变化会通知你。只有一个小小的程序文件，功能靠插件扩展。

[English](README.md)

> **状态：早期，尚未发布。** 除发布相关的工作外，M0、M1 和 M2 都已经可以从源码运行（见[从源码运行](#从源码运行)），但还没有可以安装的发布版。这份 README 记的是我们要做什么，以及刻意不做什么。

## 它会是什么样

- **这台机器的东西都在一处。** 侧栏列出本机的各个网页界面（Grafana、跑在 `localhost:3000` 的自托管工具……），点开就整页显示；首页是一组状态卡片。
- **不让嵌入的服务也能嵌进来。** 很多工具会发 `X-Frame-Options` 或 CSP 的 `frame-ancestors` 来禁止被嵌入。hostdeck 在本机给每个服务另开一个端口重新提供它，只放宽这一条限制，而且只对控制台自己放宽。不用逐个改服务的配置。
- **认得它所在的机器。** 内置 launchd 任务或 Windows 服务、Docker 容器、哪个进程在监听哪个端口的视图，可以直接重启。
- **有变化就告诉你。** 你选定关注的卡片变黄、变红或恢复，连续两次检查都看到后，通过一条命令、一个 Webhook 或系统通知推给你。
- **靠插件，不堆集成。** 每张卡片都是一个插件：大多数写几行 YAML 就够，剩下的用一个外部程序。
- **小。** 一个 Go 写的程序文件，提供一套朴素的网页界面，在浏览器里用；之后再加一层很薄的桌面壳。

## 不打算做的

- **从别的设备访问**（局域网、手机）、登录、HTTPS。hostdeck 只监听 127.0.0.1，整个安全模型都建立在这一点上。
- v1 之前不支持 **Linux**，不做 Docker 镜像，不做多用户。
- **从网络上安装插件**，以及第三方插件市场。网页上的插件页只能添加 hostdeck 自带的插件，按网址内嵌本机或局域网网页的插件，以及链接。
- **几百个内置集成。** 常见服务用示例插件覆盖。
- v1 之前不做**在网页里编辑配置**，添加内置插件、按网址内嵌本机或局域网的网页、添加链接、修改页面的名称、图标或地址、停用和启用插件、把插件移入回收区并在那里清除、排列、隐藏或重置侧栏和卡片，定时脚本任务页面写它的 `tasks.json` 和脚本，关注或取消关注卡片（只写 `notifications.json` 的 `watch` 列表），以及保存外观（只写 `appearance.json`），这些都除外。
- **Electron。**

## 路线图

每个里程碑都单独发布。M0 之后先做 M2，再给 M1 加新东西。

### M0 底座

- macOS 和 Windows 上的本机服务，只有这台机器能访问
- 英文和简体中文界面，可以切换
- 侧栏列出本机的网页界面，点开整页显示，禁止被嵌入的服务也能显示
- 每个内嵌的服务都有一个「清除缓存」按钮，只清这一个服务的缓存和网站数据，cookie 和其他服务都不动
- 内嵌的服务可以发浏览器通知：一个弹窗先为控制台询问一次，管页面打开期间的通知；再为这个服务自己的端口询问一次，管 hostdeck 关闭时的推送通知
- 首页的状态卡片
- 侧栏条目和卡片可以拖动排序，卡片可以加宽到两列，排好的布局存在 `layout.json` 里
- 插件可以从侧栏和卡片里隐藏，布局可以重置；这两件事都能撤销，记在 `layout.json` 里
- 可以修改页面的名称、图标或地址；清单里的注释和命令都保留，内嵌地址会再检查一次是不是本机或局域网
- 可以添加指向任意网页的链接；它不会被内嵌，清单里也没有命令
- 可以把插件移入回收区，并在那里清除；放回时文件夹恢复为启用，转发端口仍属于它的 id
- 插件清单 v1：一个插件就是一个文件夹，清单里写明它的页面、卡片、详情页和操作
- 声明式插件：写明一个地址、把返回 JSON 的哪些字段放进卡片、到什么阈值算黄算红
- 外部程序插件：任何把卡片 JSON 打印出来的可执行文件
- 详情页列出条目，条目上的操作要再点一次确认
- 几个常见服务的示例插件
- 插件页：不联网，从 hostdeck 自带的插件里添加，按网址或拖入内嵌本机或局域网的网页，并能停用和启用已安装的插件
- macOS 和 Windows 两个平台的 CI，以及两个平台的发布压缩包

### M1 本机视角

- macOS 的 launchd 任务和 Windows 服务，可以重启
- 哪个进程在监听哪个端口
- Docker 容器，可以重启
- 这台机器上的所有进程，只读
- 从 shell 启动的服务，可以停止和重启
- 跟随服务的日志，采集重启后服务的输出
- 磁盘变化：立即或每小时扫描文件夹，并保存扫描的结果
- 定时脚本任务（[docs/TASKS.zh-CN.md](docs/TASKS.zh-CN.md)）

### M2 推送

- 由你选定关注哪些卡片；只有这些卡片在后台检查，没有打开控制台页面时也检查
- 连续两次检查都看到变化才算数，恢复也一样
- 通过你指定的一条命令或一个 Webhook 推送；示例演示用命令发系统通知
- 记住上次推过什么，重启后不重复推
- 控制台显示后台检查是否在正常运行，以及每张关注卡片上次检查的时间

### M3 桌面版

- 桌面窗口，显示的页面和浏览器里一样
- 由 hostdeck 自己发的系统通知
- 托盘图标
- 在 macOS 和 Windows 上开机自启

### M4 脚本插件

- Starlark 插件，给几行 YAML 写不下的卡片用
- 只有清单里声明了，插件才能访问网络、执行命令

## 从源码运行

hostdeck 还没有发布。想先试试现在已有的功能，需要 Go 1.26 或更新的版本：

```sh
git clone https://github.com/Ike-li/hostdeck
cd hostdeck
go run .
```

然后在浏览器里打开 http://127.0.0.1:4678/ 。

插件放在 hostdeck 文件夹下的 `plugins/` 里：macOS 上是 `~/.config/hostdeck`，Windows 上是 `%LOCALAPPDATA%\hostdeck`。用 `--dir` 或 `HOSTDECK_DIR` 可以换一个文件夹，用 `--addr` 可以换成 127.0.0.1 上的另一个端口。

## 控制台

总览的「＋ 添加页面或插件」卡片可以直达网页、链接和内置插件的添加区域。侧栏的「页面与插件」页先显示「已启用」，再显示停用条目、需要处理的问题和「添加内容」，可以添加 hostdeck 自带的插件，也能停用和启用已安装的插件，改动立即生效：停用就是把插件文件夹改名成以点开头，hostdeck 会跳过它，里面的内容不变。隐藏只影响总览和侧栏，「恢复到工作区」可以恢复显示；启停接入不会启动或关闭外部服务。手动增加或修改的插件文件夹，可以运行 `hostdeck plugin reload` 读取，也可以重启 hostdeck 或等插件页下一次改动。本机或局域网里的网页可以在同一页按网址内嵌，也可以把网址拖到 hostdeck 上；这会生成一个插件文件夹，其中的 `plugin.yaml` 只有 `page.proxy`。拖动控制台内部的链接，或拖入控制台自身的网址，不会添加页面。

总览只在保存了顺序、卡片加宽或隐藏条目时显示“重置布局”。重置清除这些偏好，撤销可以恢复。

“添加内容”先显示网页表单和内置插件，再显示作为辅助说明的“接入教程”，说明网页、内置插件和自定义插件的接入步骤。网页与插件各有一键复制按钮，给本机 agent 的提示词包含当前控制台的实际 Origin、插件绝对目录和运行程序路径、macOS shell 和 Windows PowerShell 的原生 CLI 示例，以及验证步骤。复制不会修改数据；无法访问剪贴板时，展开并选中提示词以便手动复制。通过 API 或 CLI 添加立即生效，手动改文件后可以用 `plugin reload` 重新加载。

每个已启用且未隐藏的接入，都会同时出现在侧栏和总览。Ollama 这类只有监控卡片的插件，点击侧栏会打开 hostdeck 生成的监控视图。没有监控能力的网页、链接或详情插件，也会有总览入口卡片，显示用途和地址，不假定外部服务在线。清单有问题的插件仍显示错误。主动隐藏或停用仍会将接入移出工作区。

### 外观

侧栏底部的“外观”提供默认蓝、森林绿、暮紫和暖砂四套完整配色，以及跟随系统、浅色和深色模式。可以用颜色选择器或 `#RRGGBB` 输入自己的主色；明暗模式会分别调整亮度，让文字、焦点和按钮状态保持清晰。弹窗展示实际应用的颜色。主题只作用于 hostdeck，嵌入的服务网页保留自己的外观。

修改即时预览，点击“保存”后，同一个 hostdeck 配置目录的所有浏览器共用；可见窗口每 15 秒同步，重新获得焦点时立即同步。编辑中的窗口保留自己的预览，取消或 Escape 恢复最新已保存的外观。“恢复默认”也要保存后才写入。并发编辑以最后一次成功保存为准。

配置保存在 hostdeck 文件夹的 `appearance.json`；不同 `--dir` 实例独立。缺失时使用默认蓝、跟随系统，首次保存才创建文件。损坏或不支持的配置保留原文件并提示，明确保存才替换。写入失败保留原配置和当前草稿。手改文件后重启 hostdeck 加载。格式如下（省略 `accent` 使用预设主色）：

```json
{
  "version": 1,
  "preset": "forest",
  "mode": "system",
  "accent": "#246b4b"
}
```

`preset` 是 `default`、`forest`、`dusk` 或 `sand`；`mode` 是 `system`、`light` 或 `dark`；`accent` 可选，必须是六位十六进制颜色。未知字段、版本或取值会被拒绝。第一版只定制颜色，不接受任意 CSS、图片或主题导入。

## 编写插件

一个插件就是一个放着 `plugin.yaml` 的文件夹。两个小例子：

```yaml
# plugins/grafana/plugin.yaml：把 Grafana 放进侧栏，内嵌显示
schema: 1
name: Grafana
page:
  proxy: http://127.0.0.1:3000/
```

```yaml
# plugins/health/plugin.yaml：一张卡片，显示某个 JSON 接口里的一个值
schema: 1
name: 我的服务
card:
  http:
    url: http://127.0.0.1:8080/health
  value: status
  label: {en: Status, zh-CN: 状态}
```

卡片还可以变黄变红、用几行列出更多数值，并带上一个不写在文件里的令牌：

```yaml
# plugins/queue/plugin.yaml
schema: 1
name: 任务队列
card:
  interval: 30s          # 一次读数保留多久；不写就是 1m
  http:
    url: http://127.0.0.1:8080/stats
    headers:
      Authorization: Bearer ${env:QUEUE_TOKEN}
  value: queue.waiting   # JSON 返回里的路径，用 gjson 的写法
  label: 排队中
  warn: {above: 50}
  error: {above: 200}
  rows:
    - name: 今天失败
      value: failed.today
      error: {above: 0}
    - name: 工作进程
      value: workers.state
      warn: {not: running}
```

- `warn` 和 `error` 各写 `above`、`below`、`equals`、`not` 中的一个。两个都满足时，卡片是红的。
- `url` 和 `headers` 里可以写 `${env:NAME}`。它的值会发给服务，卡片上的报错里只显示成 `***`。启动 hostdeck 时这个环境变量必须已经设置。
- 某个插件的 `plugin.yaml` 写错了，它会显示成一张写明哪里错了的错误卡片，其他插件照常加载。

路径的写法见 [gjson](https://github.com/tidwall/gjson/blob/master/SYNTAX.md)。[`examples/`](examples) 里有可以直接复制的完整插件，其中一个的卡片和详情页由脚本生成。

发布第一个版本之前，清单格式还可能变。[docs/PLUGINS.zh-CN.md](docs/PLUGINS.zh-CN.md) 写全了：`plugin.yaml` 的每个字段，hostdeck 怎么运行插件的程序，程序又该打印什么。

## 推送

你关注的卡片有变化时，hostdeck 可以运行你指定的一条命令，或者发给一个 Webhook，没有打开控制台页面时也一样。在 hostdeck 的文件夹里放一个 `notifications.json`：

```json
{
  "watch": ["grafana", "jobs"],
  "command": ["sh", "notify.sh"],
  "windows": ["powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", "notify.ps1"]
}
```

```sh
# notify.sh，和 notifications.json 放在一起：发一条 macOS 通知。文字作为参数交给
# osascript，所以名称或值里写了什么都改变不了脚本。
osascript -e 'on run argv' -e 'display notification (item 1 of argv) with title "hostdeck"' -e 'end run' \
  "$HOSTDECK_NAME: $HOSTDECK_FROM → $HOSTDECK_TO"
```

Windows 上由 `windows` 运行 `notify.ps1`，发一条 Windows 通知。脚本只用 ASCII 字符，注释也是英文：Windows PowerShell 读没有 BOM 的脚本时按系统代码页解码。

```powershell
# notify.ps1, next to notifications.json: a Windows notification, shown as
# Windows PowerShell's. Keep it ASCII: Windows PowerShell reads a script
# without a BOM in the system's code page.
$null = [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
$null = [Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime]
$text = "${env:HOSTDECK_NAME}: ${env:HOSTDECK_FROM} $([char]0x2192) ${env:HOSTDECK_TO}"
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml("<toast><visual><binding template=""ToastGeneric""><text>hostdeck</text><text>$([Security.SecurityElement]::Escape($text))</text></binding></visual></toast>")
$app = '{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe'
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($app).Show([Windows.UI.Notifications.ToastNotification]::new($xml))
```

- 这些通知在 macOS 上记在「脚本编辑器」名下，在 Windows 上记在 Windows PowerShell 名下。macOS 只在脚本编辑器里询问是否允许：在脚本编辑器里运行一次 `display notification "hostdeck"`，再到「系统设置 › 通知」里允许脚本编辑器的通知；开着专注模式时不会显示。以 hostdeck 自己名义发的通知随桌面版（M3）一起来。
- `watch` 列出插件 ID，也就是插件文件夹的名字。只有这些卡片会在后台读取，各按自己的 `interval`。没有加载的插件、没有卡片的插件会被跳过。
- 卡片的状态是 `ok`、`warn`、`error`，读取失败时是 `unreadable`。没设阈值的卡片只要读得到就是 `ok`。
- 连续两次读数都看到的变化才算数，恢复也一样，所以读数之间来回跳的卡片什么都不会推。第一次读到就是 `ok` 的卡片也不推。
- 命令在 hostdeck 的文件夹里运行，不经过 shell，最多 30 秒，到时连同它启动的所有进程一起结束。Windows 上用 `windows` 代替 `command`，和 `plugin.yaml` 一样。它拿到 hostdeck 的环境变量，再加上这些：

| 变量 | 值 |
| --- | --- |
| `HOSTDECK_ID` | 插件的 ID |
| `HOSTDECK_NAME` | 卡片的英文名称，只有一个名称时就是它 |
| `HOSTDECK_NAME_ZH_CN` | 卡片的简体中文名称，没有时同上 |
| `HOSTDECK_FROM` | 上次告诉命令的这张卡片的状态：第一次读到就正常的卡片是 `ok`，两者都没有时为空 |
| `HOSTDECK_TO` | 连续两次读数都看到的状态 |
| `HOSTDECK_VALUE` | 卡片的值；`unreadable` 时为空 |
| `HOSTDECK_TIME` | 让命令运行的那次读数的读取时间，UTC，例如 `2026-10-29T13:01:00Z` |

Webhook 收到的是同一个变化，以 JSON 发送，可以代替命令，也可以和命令一起用：

```json
{
  "watch": ["grafana", "jobs"],
  "webhook": {
    "url": "https://hooks.example.com/hostdeck",
    "headers": {"Authorization": "Bearer ${env:HOOK_TOKEN}"}
  }
}
```

- `url` 是 http 或 https 地址；`headers` 随请求一起发送。两者都可以写 `${env:NAME}`，和 `plugin.yaml` 一样，整个 `url` 也可以就是一个：值会发出去，在任何错误里显示为 `***`，启动 hostdeck 时必须设好这个变量。
- 请求体是一个 JSON 对象，`Content-Type: application/json`，每个键的值和命令拿到的变量相同，例如 `{"id": "grafana", "name": "Grafana", "nameZhCN": "Grafana", "from": "ok", "to": "error", "value": "", "time": "2026-10-29T13:01:00Z"}`：

| 键 | 值 |
| --- | --- |
| `id` | 同 `HOSTDECK_ID` |
| `name` | 同 `HOSTDECK_NAME` |
| `nameZhCN` | 同 `HOSTDECK_NAME_ZH_CN` |
| `from` | 同 `HOSTDECK_FROM`，指上次告诉 Webhook 的状态 |
| `to` | 同 `HOSTDECK_TO` |
| `value` | 同 `HOSTDECK_VALUE` |
| `time` | 同 `HOSTDECK_TIME` |

- 写了 `body` 时，发的就是它：任意 JSON 值，其中的字符串可以写 `{id}`、`{name}`、`{nameZhCN}`、`{from}`、`{to}`、`{value}` 和 `{time}`，填进上表里的值。不管这些值里有什么，发出去的都是合法 JSON。对象的键按原样保留，但不保留顺序，也不填占位符。hostdeck 不认识的占位符，比如 `{nmae}`，会让整个文件被拒绝。所以接受 JSON 的聊天机器人，比如飞书或 Slack 的，可以直接发；要求每条消息签名的，还是用命令。

```json
"webhook": {
  "url": "https://open.feishu.cn/open-apis/bot/v2/hook/${env:LARK_HOOK}",
  "body": {"msg_type": "text", "content": {"text": "{name}: {from} → {to} ({value})"}}
}
```

- Webhook 在 30 秒内回 2xx，就算推出去了。其他回应都算失败，重定向也算，而且不会跟随。回了 2xx 但响应体里写着错误的接收端，也算已推送。

两者共同的：

- 命令和 Webhook 各推过什么，分开记在 `data/notifications/sent.json` 里，重启后不会再推一次。命令启动不了或以错误退出，或者 Webhook 失败，就算没推出去：卡片下次读取时只重试失败的那个，失败原因写在 hostdeck 的日志里。
- `notifications.json` 在 hostdeck 启动时和 `hostdeck plugin reload` 时读取。文件有错，比如有不认识的键、写了 `watch` 却既没有 `command` 也没有 `webhook`，或者 `${env:NAME}` 的变量没有设置，就拒绝并在日志里写一行，之前加载的继续生效。
- 控制台只写 `watch` 列表：卡片的更多操作里有**关注**和**取消关注**，立即生效，文件会按两个空格缩进写回。`notifications.json` 既没有 `command` 也没有 `webhook`，或者读不了时，这两个操作会被拒绝，什么都不写；控制台从不写命令或 Webhook，也不会创建这个文件。只打开页面或链接的卡片，以及清单有错的插件，没有可关注的东西。
- 概览在每张关注的卡片上写明上次检查的时间、上次推送了什么以及什么时候，还有命令或 Webhook 上次失败的原因；两者推送的内容不一样时，分别写明各自推送了什么。`notifications.json` 被拒绝，或者关注了不在这里的卡片时，卡片上方会说出来。这些每 15 秒读一次。

## 内置插件

hostdeck 自带的插件里，下面这些管的是这台机器本身，都在「页面与插件 → 添加内容」里添加。

### 后台任务

「后台任务」列出当前用户已加载的 launchd 任务，不含 Apple 自己的，也不含普通应用；在 Windows 上则是程序不在 Windows 目录里的服务。正在运行的可以重启，没在运行的可以运行。

在 Windows 上，后台任务数量同时计入警告和错误。只有可再次启动、允许停止且正在运行或已暂停的服务才提供重启。路径会先展开和规范化，再过滤 Windows 系统服务。

后台任务支持按部分名称或 ID 搜索，并按运行中、未运行、需要处理筛选，Windows 另有已禁用分类。数量随搜索更新，刷新和切换语言保留本次页面会话的筛选。

后台任务以紧凑字段展开，运行状态和操作放在名称旁。macOS 补充 PID、配置命令、启动规则、配置和两种日志路径；Windows 补充服务身份、PID、启动方式、账户和命令。列表不含日志正文。展开一条任务可以看到最多 256 KiB、能够上下滚动的日志末尾；Windows 上则是最多 100 条匹配的服务事件。运行和重启会说明随后读到的状态。可选信息读不到时在该任务内说明。

“后台任务”提供持续有界读取的“日志与状态”，以及独立的“最近操作”。操作返回与服务运行状态分开展示。已有日志原地读取；Windows 展示服务管理事件，不是应用业务日志。不修改任务或服务配置。不会覆盖已安装插件；本机修改过或旧版后台任务辅助脚本继续使用兼容的单次日志视图，直到用户明确更新这些文件。

### 监听端口

「监听端口」列出本机正在监听的每一个 TCP 端口，以及绑定地址（127.0.0.1 与 0.0.0.0）、PID、进程名、工作目录与启动命令（超过 512 字符截断）。详情页可搜索，并按绑定地址分组。hostdeck 自身、其祖先进程以及转发端口会标记但仍列出。确认后可结束监听进程及其子进程；hostdeck 自身、其祖先进程、转发端口以及 PID 1 均受保护，无法在这里结束。不列出 UDP。

“监听端口”和“手动启动的服务”使用与后台任务相同的“日志与状态”。确认 PID 与启动时间后可关联已有后台任务来源；macOS 仅检查进程标准输出／错误实际指向的普通文件。终端输出仍留在终端。Windows 无法可靠识别外部应用日志时会说明原因。

### Docker 容器

内置 **Docker 容器** 插件提供紧凑的可搜索容器列表、带数量的状态筛选与总览统计。通过已安装的 Docker 命令行展示选定元数据和有上限的最近标准输出/错误日志。引擎需已在本机运行，拒绝远程端点，安装不会启动 Docker 或容器。支持本机 Unix socket、命名管道和字面量回环 TCP 端点。确认后可启动已停止或刚创建的容器、重启运行中的容器。暂停和过渡状态不提供操作；执行前重新核对完整容器 ID、当前连接和状态。

### 进程

“进程”列出 hostdeck 可见的进程，包括没有监听端口的程序。概览显示进程数量；详情显示进程名、PID、父进程 PID、用户、CPU 和以 MiB 表示的驻留内存，并补充可执行文件路径、父进程名称、启动时间、运行时长、线程数和累计 CPU 时间。启动时间明确使用 UTC；时长按“时:分:秒”显示，小时可超过 24。可按名称或 PID 搜索，筛选本控制台或其他进程；刷新读取新的快照并保留搜索与筛选。概览数量缓存一分钟，详情按需读取。

插件只读，不结束进程、不请求管理员权限、不读取启动参数或环境变量，也不保存进程历史。可选字段因权限等原因无法读取时会明确标注，进程仍保留；进程表读取失败会显示错误，不显示为零。macOS 使用 `ps` 报告的 CPU 百分比和 RSS，通过一次可选的 `top` 快照读取线程数（最多两秒、1 MiB 输出）；Windows 以至少 250 毫秒间隔采样进程 CPU 时间，内存使用工作集。CPU 的 100% 代表占满一个核心，可以超过 100%。两种平台的读数都是估计值，不保证与活动监视器或任务管理器一致。Windows 用户列显示所有者 SID，不联网查询账户名，并排除 PID 为 0 的空闲时间伪进程。刷新得到的是动态快照，进程可能在字段读取期间退出。

### 手动启动的服务

「手动启动的服务」列出从终端或会话里启动、正在监听 TCP 的进程，例如留在终端、iTerm、Windows 终端、VS Code 或 Claude 会话里的程序。页面显示工作目录和启动命令。确认后可以结束这个进程，或从本控制台重新启动；本控制台自己不能在这里结束。从这里重新启动的进程，关掉原来的终端也不会停。hostdeck 退出后它仍在运行，并离开这个列表。launchd 任务、Windows 服务和应用自己开的端口不在这里。卡片写出每个进程和它占用的端口，页面按启动来源分组。若要看本机每一个 TCP 监听端口，见「监听端口」。

确认重启手动服务后，同一程序的独立进程会采集后续标准输出／错误。日志准备和 128 MiB 目录容量检查先于结束原服务；准备失败保留原服务，新进程启动失败明确报告，不自动重试。控制台正常或异常退出后，服务与采集仍继续；服务离开当前列表后，仍可从最近操作查看经过验证的 PID、退出结果及有界输出。每次运行用两个 2 MiB 文件轮转，完成后每个服务最多保留三次、七天。外部已有日志不做完整归档。这些数据全部位于实际 `--dir`／`HOSTDECK_DIR` 目录的 `data/service-observations/`。

### 磁盘变化

「磁盘变化」和显示剩余容量的「磁盘空间」是两个插件。它直接运行在 macOS 或 Windows 的 hostdeck 进程内，不需要 Python、数据库服务、额外端口或单独安装 disk-history。安装和打开页面都不会扫描。

打开详情页，确认「立即扫描」，或选择「开始每小时跟踪」，立即扫描一次，之后在 hostdeck 运行期间约每小时扫描。「暂停每小时跟踪」保留历史；「取消扫描」也会暂停跟踪并保留上次快照。关掉页面不会中止扫描。页面打开时每两秒检查正在运行的扫描，完成后自动显示保存的结果；空闲页面每分钟检查一次，也可以手动刷新。只有一次读数时，卡片说明怎样看到变化。读数超过两小时，只有在开启每小时跟踪时才算注意。退出 hostdeck 会中断当前扫描，已开启的跟踪在下次启动后恢复。停用或移入回收区会取消扫描及定时任务，再启用时恢复保存的跟踪状态。隐藏只影响工作区布局。

默认扫描当前用户的主目录。需要其他目录时，在 hostdeck 数据目录里的 `plugins/disk-history/config.json` 配置 1–8 个绝对目录路径。Windows 的 JSON 路径需要转义反斜杠：

```json
{"roots":["/absolute/path/to/folder"]}
```

```json
{"roots":["C:\\Users\\Example\\Documents","D:\\Projects"]}
```

文件改动对后续扫描生效，正在运行的扫描继续处理原目标。路径只从本地配置读取，网页请求不能提交扫描路径。每个目标单独保存历史。总览展示第一个目标及其一级目录中变化最大的三项，时间取整到小时（两次读数相隔不到一小时时按分钟）；目标是主目录时写成「过去 24 小时 · 主目录共 122.5 GB」，行名形如 `~/code`；独立详情页可选择已配置目标，以及 1 小时、24 小时（默认）、7 天或 30 天范围。居中阅读区先展示当前大小和变化，再展示大小曲线、分时段变化和按增减绝对值排序的直接子目录。选中柱子只比较该时段；打开目录可以逐层查看，通过面包屑返回。列表先显示 20 个目录，其余可展开。范围以最新保存的扫描为终点，基准使用范围起点之前最近的扫描，没有时用最早记录；始终展示实际比较时间，采用浏览器本地时区。曲线保留实际采样时间，不可比较处断开，缺失记录不会编造成删除量。「扫描记录与统计说明」可查看所选范围的记录值。三层边界和摘要上限在页面中明确说明。打开目录只读取已保存的摘要，不会扫描、添加目标或导入旧 homedeck 历史。

统计普通文件实际占用的磁盘空间，采用十进制单位。稀疏文件只计实际占用的块，所以 Docker 这类磁盘镜像按实际存储计算，而不是按文件长度。macOS 上一个文件有多个硬链接名称时只计一次。Windows 上稀疏文件和压缩文件计实际存储的字节，其他文件计文件长度，每个硬链接名称单独计数。APFS 克隆的每份副本都完整计入。改为统计磁盘空间之前保存的读数记的是文件长度，不与新读数比较，随其余历史一起按期清除。目录摘要最多三层，总量包含更深层文件。跳过链接、Windows 重解析点、其他挂载卷和插件自身文件。扫描是实时遍历，并非原子文件系统快照。读不到的条目（例如 macOS 隐私保护不让 hostdeck 读取的目录）不计入读数，卡片注明个数。两次读数读不到的是同一批条目时，照常比较读到的部分；读不到的条目不同时，不给出增减，卡片说明原因。扫描途中被删除的条目算作已不存在，不算读不到。失败或取消的扫描不会替换已有快照。每次扫描最长 20 分钟。

历史保存在插件的 `data/history.json`，独立于 homedeck 原来的 SQLite 数据。保留最近 30 天、每个目标最多 744 次快照、每次最多 256 条目录摘要，总文件不超过 64 MiB。目录摘要达到上限时仍保留根目录总量，优先保留浅层摘要，详情会说明。历史损坏会报错，不会自动覆盖。进程持有的操作系统锁阻止第二个写入者，进程退出后自动释放。备份插件文件夹即可保留历史；从回收区彻底清除插件时也会删除历史。

编译进程序的插件使用 `exec.command: ['@hostdeck', disk-history, card]`，另外两个操作为 `detail` 和 `action`。`@hostdeck` 是这些操作的进程内保留分发标记，不从 PATH 查找程序，也不启动 shell。

### 定时脚本任务

内置的「定时任务」插件（`scheduled-tasks`）安装空的 `tasks.json`。详情页可以新建、编辑和删除任务，这会改写 `tasks.json` 并立刻刷新调度，也可以把脚本文件保存到该插件的 `scripts/` 目录。保存脚本不会添加任务，也不会运行。脚本行可以按允许的命令运行一次而不写 `tasks.json`，或打开预填了默认命令的任务表单。手改 `tasks.json` 后执行 `plugin reload` 仍然会加载。已提供确认后的手动运行和停止、超时、结果、最近十次运行摘要及有界日志，Cron 和固定间隔计划在 hostdeck 运行期间调度。暂停／恢复控制后续启动，停止针对一次运行；不补跑停机任务，也不自动重试。配置在启动或显式 `plugin reload` 时加载；被拒绝的配置保留上一份有效配置及当前运行，并报告错误。读取不启动脚本，脚本输出不要求 JSON；非零退出和超时分别报告。运行只写插件自己的 `data/`，已知引用密钥在日志中遮蔽。字段与规则见 [TASKS.zh-CN.md](docs/TASKS.zh-CN.md)。

遗留运行标为中断并暂停，恢复只查询进程身份是否清空，不能结束旧 PID；未确认清理的任务不重新启动。所有者锁排除第二写入者，损坏状态拒绝覆盖。历史／日志清理先保存引用变更，写入失败保留原来的日志。结果不能落盘时明确显示中断及存储错误。调度持久保存锚点、游标及错过统计；每任务互斥，每个已安装插件最多四并发。

安装同时提供停用的 `tasks.example.json`；空详情说明绝对路径，并写明可以在页面上新建任务或脚本，手改后重载仍然有效。连接帮助的插件提示词包含完整任务格式和当前控制台可执行的命令。

## 给 agent 使用的 CLI

保持控制台运行，CLI 使用相同版本的 hostdeck。接入命令不会启动另一个控制台。`--url` 选择控制台 Origin，默认 `http://127.0.0.1:4678`；只接受 `127.0.0.1` 上的 HTTP，拒绝其他主机、凭据、URL 路径和重定向。界面里的复制提示词会使用当前程序的绝对路径，即使用 `go run` 启动也一样。

从本仓库运行时，把下面的 `hostdeck` 换成 `go run .`。编译后的程序不在 PATH 时使用实际路径，例如 macOS 的 `./hostdeck`，或 PowerShell 的 `& .\hostdeck.exe`。

```sh
hostdeck help
hostdeck page add --url http://127.0.0.1:4678 --address http://127.0.0.1:3000/ --name Grafana --mode proxy
hostdeck page add --url http://127.0.0.1:4678 --address https://example.com/ --name Notes --mode link
hostdeck page update --url http://127.0.0.1:4678 --id PAGE_ID --name "New name" --icon N
hostdeck plugin list --url http://127.0.0.1:4678
hostdeck plugin catalog --url http://127.0.0.1:4678
hostdeck plugin install --url http://127.0.0.1:4678 --id ollama
hostdeck plugin validate --from /path/to/my-plugin
hostdeck plugin install --url http://127.0.0.1:4678 --id my-plugin --from /path/to/my-plugin
hostdeck plugin disable --url http://127.0.0.1:4678 --id my-plugin
hostdeck plugin enable --url http://127.0.0.1:4678 --id my-plugin
hostdeck plugin reload --url http://127.0.0.1:4678
```

命令后使用具名参数。`page update` 只修改传入的 `--name`、`--icon`、`--proxy` 或 `--link`，以及用于已有 HTTP 卡片的 `--card-url`；proxy 与 link 不能同时传入。注释、命令和无关配置会保留。`plugin list` 与 `plugin catalog` 都返回完整目录信息，包括实际路径、已安装条目和可用内置插件。

`plugin validate` 离线运行，使用与服务端相同的 `schema: 1` 解析器和 CLI 进程环境，不执行插件程序，也不访问服务。自定义安装的源文件夹必须在本机，包含 `plugin.yaml`；CLI 向选定控制台查询实际插件目录，校验后先复制到隐藏的临时文件夹，再放到目标位置并重新加载。已有启用或停用的同名 ID、符号链接、特殊文件，以及包含目标目录的源文件夹都会被拒绝。不下载插件。每个 ID 有安装锁，CLI 安装相互串行；进程中断可能留下锁或隐藏临时文件夹，确认进程结束后再手动清理。备份和源文件夹应放在活动插件目录之外。

所有接入命令，包括 `help`，都向 stdout 输出一份 JSON。成功包含 `ok: true` 和 `data`，失败包含 `ok: false` 和 `error.code`、`error.message`，有时还有恢复信息。退出码 **0** 为成功，**1** 为操作或校验失败，**2** 为参数错误。PowerShell 每次调用原生命令后都要检查 `$LASTEXITCODE`。文件已保存但重载失败时，`data.saved` 为 true；本机自定义安装还会用 `data.path` 给出文件位置，API 修改在 ID 已知时返回 ID；修复报错后重新加载，不要重复安装。`plugin reload` 遇到损坏插件或转发错误返回非零；重载发生致命错误时保留之前的加载状态。

`data` 里有什么：

- `plugin validate`：`data.valid`、`data.schema`（这个 hostdeck 读取的清单版本）、`data.path`（文件夹的绝对路径）和 `data.name`。
- `page add`、`page update`、`plugin install`、`plugin enable` 和 `plugin disable`：`data.id`、`data.path`（插件的文件夹，停用时以点开头）、`data.saved`、`data.loaded`、`data.disabled`、`data.available`（已启用，并且加载时没有出错）和 `data.plugin`，即控制台自己对这个插件的记录。
- `plugin list`、`plugin catalog` 和 `plugin reload`：控制台的插件目录。其中的 `pluginDir` 和 `installed` 列表（每项的 `id`、`loaded`、`disabled`、`broken`、`error`）是 CLI 自己会读的部分。

`error.code` 要么是 CLI 自己的错误码，要么是控制台返回的错误码；后者在 [web/i18n/zh-CN.json](web/i18n/zh-CN.json) 的 `error.<code>` 下有说明：

| 错误码 | 含义 |
| --- | --- |
| `usage` | 命令、某个参数或 `--url` 不对；退出码为 2 |
| `invalid_manifest` | `--from` 里的 `plugin.yaml` 加载不了；消息里写了原因 |
| `invalid_source` | 源文件夹读不了，里面有链接或特殊文件，或者包含了插件目录 |
| `already_added` | 已经有启用或停用的插件用了这个 ID |
| `install_busy` | 同一个 ID 的另一次安装正在进行 |
| `install_failed` | 复制或放置文件夹失败 |
| `connection_failed` | `--url` 上没有控制台应答 |
| `request_failed` | 请求没能发出，或者控制台拒绝了它但没给错误码 |
| `invalid_response` | 控制台的回答不是这个 CLI 能读的，比如版本不同的控制台 |
| `not_loaded` | 文件已经保存，但插件没有加载；看 `data.plugin` |
| `plugins_unavailable` | `plugin reload` 做完了，但有启用的插件损坏或没有加载；看 `data.installed` |

从第一个正式版本起，命令、参数、退出码，以及这里列出的键和错误码都会保留；可能新增。先看 `ok` 和退出码，不认识的键忽略，不认识的错误码当作失败处理。`error.message` 是给人看的，措辞可能变。`data.plugin` 和插件目录的其余部分跟随控制台自己的 API，任何版本都可能变。见[约定与兼容](docs/ARCHITECTURE.zh-CN.md#约定与兼容)。

`loaded` 只表示 hostdeck 加载了清单，不代表外部服务健康，需要打开卡片或网页验证实际读数。CLI 校验与服务端加载分别使用各自的环境变量；新增密钥必须提供给运行中的服务端，必要时经过授权重启。普通文件改动可以通过 `plugin reload` 生效，无需重启。校验和复制本身不运行脚本，之后的卡片或详情请求仍按原协议执行可信插件程序。网页仍然不能提交或写入命令。

## 工作原理

[docs/ARCHITECTURE.zh-CN.md](docs/ARCHITECTURE.zh-CN.md) 讲了各个组成部分、每种请求的经过，以及安全模型。[docs/PLUGINS.zh-CN.md](docs/PLUGINS.zh-CN.md) 是插件的约定。[docs/DESIGN.zh-CN.md](docs/DESIGN.zh-CN.md) 是控制台的设计规范：令牌、组件和通用模式，每项都写了取值、理由和守住它的测试。

## 平台

macOS 和 Windows。Windows 在有人实际用过之前标为**实验性**，目前只有 CI 覆盖。

## 许可证

[MIT](LICENSE)
