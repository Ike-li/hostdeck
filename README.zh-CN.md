# hostdeck

给你自己这台电脑用的小控制台，支持 macOS 和 Windows。本机在跑的服务、端口、容器和后台任务都在这里看、在这里管；它们的网页界面在一个地方整页打开；有什么变化会通知你。只有一个小小的程序文件，功能靠插件扩展。

[English](README.md)

> **状态：早期，尚未发布。** M0 的大部分功能已经可以从源码运行（见[从源码运行](#从源码运行)），但还没有可以安装的发布版。这份 README 记的是我们要做什么，以及刻意不做什么。

## 它会是什么样

- **这台机器的东西都在一处。** 侧栏列出本机的各个网页界面（Grafana、跑在 `localhost:3000` 的自托管工具……），点开就整页显示；首页是一组状态卡片。
- **不让嵌入的服务也能嵌进来。** 很多工具会发 `X-Frame-Options` 或 CSP 的 `frame-ancestors` 来禁止被嵌入。hostdeck 在本机给每个服务另开一个端口重新提供它，只放宽这一条限制，而且只对控制台自己放宽。不用逐个改服务的配置。
- **认得它所在的机器。** 内置 launchd 任务或 Windows 服务、Docker 容器、哪个进程在监听哪个端口的视图，可以直接重启。
- **有变化就告诉你。** 卡片变黄、变红或恢复，连续两次检查都看到后，通过一条命令、一个 Webhook 或系统通知推给你。
- **靠插件，不堆集成。** 每张卡片都是一个插件：大多数写几行 YAML 就够，剩下的用一个外部程序。
- **小。** 一个 Go 写的程序文件，提供一套朴素的网页界面，在浏览器里用；之后再加一层很薄的桌面壳。

## 不打算做的

- **从别的设备访问**（局域网、手机）、登录、HTTPS。hostdeck 只监听 127.0.0.1，整个安全模型都建立在这一点上。
- v1 之前不支持 **Linux**，不做 Docker 镜像，不做多用户。
- **从网络上安装插件**，以及第三方插件市场。网页上的插件页只能添加 hostdeck 自带的插件，按网址内嵌本机或局域网网页的插件，以及链接。
- **几百个内置集成。** 常见服务用示例插件覆盖。
- v1 之前不做**在网页里编辑配置**，添加内置插件、按网址内嵌本机或局域网的网页、添加链接、修改页面的名称、图标或地址、停用和启用插件、把插件移入回收区并在那里清除、排列、隐藏或重置侧栏和卡片除外。
- **Electron。**

## 路线图

每个里程碑都单独发布。

### M0 底座

- macOS 和 Windows 上的本机服务，只有这台机器能访问
- 英文和简体中文界面，可以切换
- 侧栏列出本机的网页界面，点开整页显示，禁止被嵌入的服务也能显示
- 每个内嵌的服务都有一个「清除缓存」按钮，只清这一个服务的缓存和网站数据，cookie 和其他服务都不动
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

### M2 推送

- 连续两次检查都看到变化才算数，恢复也一样
- 通过一条命令、一个 Webhook 或系统通知推送
- 记住上次推过什么，重启后不重复推

### M3 桌面版

- 桌面窗口，显示的页面和浏览器里一样
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

插件放在 hostdeck 文件夹下的 `plugins/` 里：macOS 上是 `~/.config/hostdeck`，Windows 上是 `%LOCALAPPDATA%\hostdeck`。用 `--dir` 或 `HOSTDECK_DIR` 可以换一个文件夹，用 `--addr` 可以换成 127.0.0.1 上的另一个端口。侧栏的「插件」页可以添加 hostdeck 自带的插件，也能停用和启用已安装的插件，改动立即生效：停用就是把插件文件夹改名成以点开头，hostdeck 会跳过它，里面的内容不变。手动增加或修改的插件文件夹，要重启 hostdeck 或等插件页下一次改动时才会读取。本机或局域网里的网页可以在同一页按网址内嵌，也可以把网址拖到 hostdeck 上；这会生成一个插件文件夹，其中的 `plugin.yaml` 只有 `page.proxy`。

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

发布第一个版本之前，清单格式还可能变。

## 工作原理

[docs/ARCHITECTURE.zh-CN.md](docs/ARCHITECTURE.zh-CN.md) 讲了各个组成部分、每种请求的经过，以及安全模型。

## 平台

macOS 和 Windows。Windows 在有人实际用过之前标为**实验性**，目前只有 CI 覆盖。

## 许可证

[MIT](LICENSE)
