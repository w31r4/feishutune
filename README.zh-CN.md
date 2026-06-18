# feishutune

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](go.mod)
[![Platform: macOS](https://img.shields.io/badge/platform-macOS-lightgrey?logo=apple&logoColor=white)](#环境要求)

[English](README.md) | 简体中文

让你的飞书个性签名实时同步**本地音乐应用**正在播放的歌曲 —— 自动识别 **网易云音乐**、
**Spotify** 或 **QQ 音乐**，仅支持 macOS。

```text
♫ Clair de Lune ♡ · Debussy  3:51 ━━━━━━━●── 5:08
```

当有歌曲在播放且你正在使用 Mac 时，签名会显示为这一行正在播放的内容：歌曲、若已喜欢
则带一个 ♡，以及两端标注已播放时长与总时长的实时进度条。否则回退到一个单词状态文案 ——
`online`（在 Mac 前）、`away`（离开超过空闲阈值）或 `weekend`（周末空闲）。

## 工作原理

每次 `update` 都是**一次性**执行：读取播放器、生成签名，**仅当内容变化时**才写入飞书。
没有常驻后台进程 —— 由一个 `launchd` 定时任务按间隔（默认每分钟）运行 `update`，而变化
检测让绝大多数定时执行都是廉价的空操作。

- **网易云音乐** 和 **QQ 音乐** 通过它们发布到 macOS 系统“正在播放”信息读取，借助
  [`media-control`](https://github.com/ungive/media-control) 命令行工具。顺序是网易云、
  Spotify、QQ 音乐。
- **Spotify** 通过 AppleScript（`osascript`）在本地读取 —— 仅限本机，不含手机或
  Spotify Connect 设备。
- **空闲检测** 使用 `ioreg`（`HIDIdleTime`）判断你是否在键盘前，从而在你离开时切换到
  离开状态。
- **飞书** 通过带 Cookie 鉴权的 `PUT` 请求更新到网页客户端所用的同一个接口 —— 这是非官方
  接口，飞书端的改动可能会让它失效。
- 喜欢歌曲上的 **♡** 是可选的。Spotify 通过你的 `sp_dc` Cookie 调用 Spotify 内部网页
  GraphQL 读取；网易云可在配置后使用官方 API，否则回退到本地缓存；QQ 音乐直接读取
  本地库。读取不到时，工具照常运行，只是不显示爱心。

本工具在设计上容错：播放器读取出错时显示空闲状态而非失败，空闲读取出错时默认你在场，
喜欢状态读取出错时仅去掉 ♡，飞书写入失败会在下一次定时执行时自然重试。

## 环境要求

- macOS（依赖 `osascript`、`ioreg`、`sqlite3` 和 `launchd`）
- 一个音乐应用：网易云音乐、Spotify 桌面版，和/或 QQ 音乐桌面版
- 使用网易云音乐或 QQ 音乐需要：[`media-control`](https://github.com/ungive/media-control)
  —— `brew install media-control`（只用 Spotify 的用户无需安装）
- 构建需要 Go 1.26+
- 一个飞书账号（已针对飞书测试；未在 Lark 国际版上验证）

## 安装

```bash
go install github.com/Durden-T/feishutune/cmd/feishutune@latest
```

会安装到 `$GOBIN`（通常是 `~/go/bin`），请确保它在你的 `PATH` 中。之后如需更新，重新运行
同样的命令即可 —— 已设置的定时任务会在下一次执行时自动运行新的二进制文件，无需重新加载。

## 配置

### 1. 保存飞书 session Cookie

在浏览器登录飞书，复制 `session` Cookie 的值，然后通过管道传入：

```bash
pbpaste | feishutune login
```

该 Cookie 有效期约 350 天，保存在 `~/.feishutune/session`。也可以通过环境变量
`FEISHU_SESSION` 传入。

### 2.（可选）启用喜欢歌曲上的 ♡

**Spotify：** 从已登录的 `open.spotify.com` 浏览器会话中获取 `sp_dc` Cookie（它是
HttpOnly 的 —— 需从开发者工具读取），然后：

```bash
pbpaste | feishutune spotify-login
```

有效期约 1 年，保存在 `~/.feishutune/sp_dc`。也可设置 `SPOTIFY_SP_DC`。

**QQ 音乐：** 无需任何设置 —— ♡ 直接从应用本地的收藏列表（我喜欢）读取，按歌名 + 歌手
匹配。只需登录 QQ 音乐应用，让收藏同步到本地即可。（由于是按文本而非稳定 ID 匹配，属
尽力而为，可能漏掉与其他版本同名同歌手的歌曲。）

**网易云音乐：** 无需任何设置 —— ♡ 从应用本地缓存里的「我喜欢的音乐」读取。有稳定歌曲
ID 时优先按 ID 匹配；没有 ID 时才严格按歌名 + 歌手 + 专辑/时长匹配，因此宁可漏掉，也
避免把其他版本误标成喜欢。

也可以保存网易云开放平台凭据，在确认官方 endpoint 后启用 API 元数据和红心增强：

```bash
cat netease-auth.json | feishutune netease-auth
```

```json
{
  "app_id": "your-app-id",
  "private_key": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----",
  "access_token": "oauth-access-token",
  "refresh_token": "optional-refresh-token",
  "expires_at": "2026-06-12T00:00:00Z"
}
```

凭据保存到 `~/.feishutune/netease-auth.json`。环境变量
`NETEASE_APP_ID`、`NETEASE_PRIVATE_KEY`、`NETEASE_OAUTH_TOKEN`、
`NETEASE_REFRESH_TOKEN`、`NETEASE_TOKEN_EXPIRES_AT` 会覆盖文件值。歌曲详情和严格
搜歌默认使用已验证存在的开放平台 path；仍可通过
`NETEASE_API_SONG_DETAIL_PATH`、`NETEASE_API_SEARCH_PATH`、
`NETEASE_API_BASE_URL` 覆盖。官方红心状态还需要
`NETEASE_API_LIKED_PATH`，直到红心歌单 path 被确认。

### 3. 试运行

```bash
feishutune preview   # 渲染当前签名，但不写入飞书
feishutune update    # 计算一次，若有变化则推送到飞书
```

### 4. 设为定时任务

```bash
feishutune install                  # 通过 launchd 每分钟运行一次 update
feishutune install --interval 30s   # 或自定义间隔
```

`install` 会把 LaunchAgent 写入 `~/Library/LaunchAgents/feishutune.plist` 并加载。
停止：

```bash
feishutune uninstall
```

## 命令

| 命令            | 作用                                                      |
| --------------- | -------------------------------------------------------- |
| `update`        | 计算一次签名，若有变化则推送到飞书                        |
| `preview`       | 打印当前签名但不写入                                      |
| `pause`         | 隐藏正在播放，改为显示在 Mac 前/离开状态                  |
| `resume`        | 恢复正在播放更新                                          |
| `status`        | 打印是否已暂停以及上次写入的签名                          |
| `login`         | 保存飞书 session Cookie（从 stdin 读取）                  |
| `spotify-login` | 保存用于 Spotify ♡ 的 `sp_dc` Cookie（从 stdin 读取）    |
| `netease-auth`  | 保存用于网易云 API 增强的凭据 JSON                        |
| `install`       | 安装一个按间隔运行 `update` 的 launchd 任务              |
| `uninstall`     | 移除 launchd 任务                                         |
| `version`       | 打印版本号                                                |

运行 `feishutune <command> -h` 查看某个命令的参数。`update` 和 `status` 支持
`--json`；`update` 还支持 `--quiet`。

## 配置项

配置按层级叠加，靠后的来源优先级更高：

```
默认值  <  ~/.feishutune/config.json  <  环境变量  <  命令行参数
```

| 配置项     | 参数           | 环境变量      | 默认值      | 含义                                       |
| ---------- | -------------- | ------------- | ----------- | ------------------------------------------ |
| Online     | `--online`     | `ONLINE`      | `online`    | 在 Mac 前且无歌曲播放时的状态文案          |
| Offline    | `--offline`    | `OFFLINE`     | `away`      | 离开 Mac 时的状态文案                      |
| Weekend    | `--weekend`    | `WEEKEND`     | `weekend`   | 周末空闲时的状态文案                      |
| Idle after | `--idle-after` | `IDLE_AFTER`  | `10m`       | 多久无操作算作离开（Go duration 格式）     |
| Blacklist  | `--blacklist`  | `BLACKLIST`   | （无）      | 逗号分隔的子串，命中则不发布               |

`~/.feishutune/config.json` 示例：

```json
{
  "online": "afk",
  "idle_after": "5m",
  "blacklist": "podcast,white noise"
}
```

命中黑名单会完全阻止发布 —— 不写入任何内容，本次执行会报告为已拦截（blocked）。

## 文件

所有文件都在 `~/.feishutune/` 下：

- `session` —— 飞书 session Cookie
- `sp_dc` —— 用于 Spotify ♡ 的 Spotify Cookie（如已设置）
- `netease-auth.json` —— 网易云开放平台凭据（如已设置）
- `config.json` —— 可选的配置覆盖
- `state.json` —— 上次写入的签名和暂停标志
- `spotify-cache.json` —— 缓存的 Spotify 令牌和各歌曲的喜欢状态
- `agent.log` —— 定时 launchd 运行的 stdout 和 stderr（便于排查问题）

（网易云和 QQ 音乐的正在播放来自 `media-control`；本地 ♡ 兜底直接读取应用自己的缓存或库。）

Cookie 以明文形式保存（不在 macOS 钥匙串中），但文件仅属主可访问 —— 目录为 `0700`，
每个文件为 `0600`。

## 退出码

| 退出码 | 含义                                       |
| ------ | ------------------------------------------ |
| `0`    | 正常                                       |
| `1`    | 其他错误                                   |
| `2`    | 用法错误                                   |
| `3`    | 飞书 session 已过期或无效 —— 需重新 `login` |

## 排查问题

- **签名没有更新。** 看 `feishutune status`（上次签名、是否暂停）和 `feishutune preview`
  （此刻会写入什么）。定时任务会把每次运行追加到 `~/.feishutune/agent.log` —— 从中查找报错 ——
  用 `launchctl list | grep feishutune` 确认任务已加载。
- **退出码 3 /「session 过期」。** 飞书 Cookie 已失效（有效期约 350 天）。重新运行
  `pbpaste | feishutune login`。
- **Spotify 歌曲没有 ♡。** `sp_dc` Cookie 缺失或过期（约 1 年）；日志会提示何时需要重新授权。
  获取新的 Cookie 后运行 `pbpaste | feishutune spotify-login`。
- **网易云歌曲没有 ♡。** 保持登录网易云应用，让「我喜欢的音乐」同步到本地缓存。API 增强是
  可选能力，需要 `netease-auth`；官方 API 红心状态还需要已确认的红心歌单 path。匹配策略较严格，
  因此某些版本可能会漏掉，而不是误标。
- **QQ 音乐歌曲没有 ♡。** 保持登录 QQ 音乐应用，让「我喜欢」同步到本地库。匹配按歌名 + 歌手进行，
  因此同一首歌的其他版本可能会漏掉。
- **未检测到网易云 / QQ 音乐。** 安装 `media-control`（`brew install media-control`）；
  任务会在 `/opt/homebrew/bin`（Apple 芯片）和 `/usr/local/bin`（Intel）中查找它。

## 开发

```bash
go build -o feishutune ./cmd/feishutune   # 本地二进制
go test ./...                          # 完整测试套件（实网测试会自动跳过）
go vet ./...
```

架构采用端口与适配器（ports-and-adapters）模式，核心领域层（`internal/bio`）为纯函数；
完整说明见 [CLAUDE.md](CLAUDE.md)。

## 许可证

[MIT](LICENSE)
