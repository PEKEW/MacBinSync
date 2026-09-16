# macsync

**本机工具/配置盘点 + Web 可视化 + 多机同步**（macOS）

自建版 Fleet：一个 Go 单二进制，本地起 Web 服务，盘点这台 Mac 装了什么（brew / uv / 运行时 / 散装工具 / GUI 应用 / 配置文件），可视化展示；每个工具可点击执行「卸载」或「加入同步队列」；未来通过 git 私有仓库把清单和配置同步到其他 Mac。

> ⚠️ **给新对话的冷启动指引见文末「冷启动」一节。** 开始动手前先读完整个 README。

---

## 快速上手

```bash
cd ~/macsync

# 1) 编译（Go 1.26，纯标准库，无外部依赖）
/opt/homebrew/bin/go build -o macsync .

# 2) 盘点本机 → ~/.macsync/current.json + reports/<主机名>.json
./macsync scan
#    可选：同时生成单文件 HTML 快照（双击即可打开，无需启动服务）
./macsync scan --html ~/Desktop/macsync-快照.html --open

# 3) 启动 Web 界面（默认 127.0.0.1:8787）
./macsync serve            # 可选 --port N
```

停止服务：`pkill -f "macsync serve"`（也可直接 Ctrl+C）。

**缓存策略（重要）**：`serve` **启动和打开页面都不会自动盘点**——只读缓存文件 `~/.macsync/current.json`，瞬时出数据；只有点页面右上角「↻ 重新盘点」或 `POST /api/report` 才触发新扫描（首次无数据时页面会显示引导按钮）。`macsync scan --html FILE` 可导出**自包含 HTML 快照**（内嵌数据，离线模式隐藏同步/队列按钮，双击即看）。

**端口占用自动处理**（`serve` 启动时）：
- 占着端口的若是**旧的 macsync 实例** → 自动停止它并接管，无需手动找进程
- 被**其他进程**占用 → 报错列出 `PID + 进程名`，并给出释放命令：`lsof -ti:8787 | xargs kill`，或 `macsync serve --port N` 换端口
- "已启动"提示在**端口绑定成功后才打印**（不会先显示启动成功再失败）

⚠️ **运行须知（重要）**：如果服务是由受限环境（如 AI 助手的沙箱）启动的，**「卸载/移入废纸篓」等删除操作会失败**（报 `Operation not permitted`，因为要删 `/opt/homebrew`、`/Applications` 下的文件）。**正常使用请从自己的终端运行 `./macsync serve`**，盘点、队列、同步等功能不受影响。

---

## 当前进度（2026-08-28 存档）

### ✅ 已完成：M0 盘点 CLI
`macsync scan` 采集 8 类数据，输出 JSON 报告：

```
正在盘点 PeikedeMac-mini.local …
  [1/8] ✓ 系统信息 (28ms)
  [2/8] ✓ Homebrew (15.1s)      ← 最慢，含 leaves/outdated 检查
  [3/8] ✓ uv 工具 (71ms)
  ...
扫描完成 → ~/.macsync/current.json（总耗时 16.9s）
```

- **实时进度**：终端下逐行原地刷新（`…` → `✓ 完成 (耗时)`），管道/重定向时退化为普通行（`isTTY()` 自动判断）
- 每步耗时、总耗时一目了然（最慢通常是 Homebrew 的 `brew outdated` 网络检查）

| 采集器 | 数据来源 | 说明 |
|---|---|---|
| 系统信息 | `sw_vers` / `uname -m` / `hostname` / `$SHELL` | 主机名、macOS 版本、芯片、Home、brew 前缀 |
| brew | `brew list --formula/--cask --versions`、`brew tap`、`brew leaves`、`brew outdated --formula --json=v2` | formula（标记 **top_level**=leaves、**outdated**）、cask、tap |
| uv | `uv tool list` | 工具名、版本、binaries |
| 运行时 | `rustup toolchain list`、node/npm（多路径探测）、`~/.nvm` | rustup toolchain、node 版本、nvm 版本、npm 全局包 |
| cargo | `~/.cargo/bin` 目录扫描 | 二进制列表 |
| ~/.local/bin | 目录扫描 | 散装 CLI（claude、synto、kimi 等） |
| GUI 应用 | `/Applications` + `plutil` 读版本 | 名称、路径、版本 |
| 配置文件 | `$HOME` 点文件 + `~/.config/*`（有排除表） | 类型 file/dir/git-repo、短 sha256、大小、文件数、修改时间 |

### ✅ 已完成：M1 Web 仪表盘
- 分类卡片：系统信息 / Homebrew / uv / 运行时 / Cargo / ~/.local/bin / GUI 应用 / 配置文件
- **Homebrew 区块**：`brew leaves`（主动安装）主表展示，**依赖公式默认折叠**（`<details>`），区块徽标显示 `N 主动安装 · N 依赖 · N cask`
- 顶部全局过滤框（过滤时自动展开依赖列表）、「重新盘点」按钮（POST /api/report）
- 前端纯原生 JS，无构建步骤，`//go:embed` 打进二进制

### ✅ 已完成：工具交互（卸载 / 同步队列）
- **点击任意工具/配置 → 弹窗 → 操作**：

| 来源 source | 卸载动作 | 入队 |
|---|---|---|
| `brew-formula` | `brew uninstall <name>` | ✅ |
| `brew-cask` | `brew uninstall --cask <name>` | ✅ |
| `uv` | `uv tool uninstall <name>` | ✅ |
| `npm` | `npm uninstall -g <name>`（npm 路径自动探测） | ✅ |
| `app` | **移动到废纸篓** `~/.Trash`（不物理删除） | ✅ |
| `cargo` / `local-bin` | ❌（由 rustup/安装器管理，删除风险高） | ✅ |
| `config` | ❌ | ✅ |

- 卸载前 `confirm` 二次确认；失败时 toast 自动提取命令输出中的 `Error:` 行（`extractErr()`）
- **同步队列**：持久化于 `~/.macsync/queue.json`（`{source, name, added_at}`，自动去重），重启不丢；头部徽标实时计数；队列查看弹窗支持逐条移除。**M3 将直接消费此队列**
- 卸载成功后自动重新盘点刷新页面

### ✅ 已完成：M3 v1 GitHub 多机同步（GUI 全程交互）

- **同步载体**：GitHub 私有仓库（gh CLI 认证，本机已登录账号 `PEKEW`；无 gh 时退化 `git ls-remote` 凭据检测）
- **点击「☁️ 同步」按钮 → 弹窗**：
  - **未配置** → 引导填写仓库（`owner/name` 或完整 URL）+ 创建私有仓库命令（`gh repo create macsync-sync --private`）+ 「保存并测试连接」
  - **已配置** → 显示连接状态/上次同步时间，「测试连接」「↑ 推送」「↓ 拉取」「更换仓库」按钮，结果区实时反馈
- **push**：本机 `queue.json` + `reports/*.json` 快照 → git（先 pull 减少冲突，`-c user.name=macsync` 提交）→ push GitHub；成功后记录 `last_sync`
- **pull**：git pull → **并集合并**远端 queue（不丢本机已选条目）→ 回拷各机报告快照
- **仓库内容**：`queue.json` + `reports/<hostname>.json`（**不含密钥配置**，configs 目录暂不同步）
- 配置存于 `~/.macsync/config.json`；克隆在 `~/.macsync/sync/`
- **已验证**：gh 已登录时 test 走 gh 认证；仓库不存在 → 明确报错"无法访问仓库…请检查仓库名"；非法 direction → 400
- 已知边界：沙箱内启动的服务 push/pull 可用，但卸载类操作仍受限（见运行须知）

### 已知问题 / 本机特性（排查时先看这里）

1. **沙箱限制**：受限环境启动的服务无法删除工作区外文件（见「运行须知」）。已用完整权限手动完成过 aerospace 卸载验证
2. **未信任 tap**：`nikitabobko/tap`（AeroSpace 官方 tap）处于未信任状态，会导致 `brew list --cask --versions` / 卸载 cask 报错（已做降级：退回纯名称列表）。如需信任：`brew trust nikitabobko/tap`
3. **brew 的 node/npm/npx 软链被删**（`/opt/homebrew/bin` 仅剩 corepack）——用户本地修改过。实际生效的是 nvm 的 node v20.20.2；`claude`（@anthropic-ai/claude-code@2.1.183）、`codex`（@openai/codex@0.148.0）是 nvm node 的 npm 全局包。采集器通过 `findNodeDirs()`（PATH → nvm 各版本最新优先 → brew opt）多路径探测
4. **密钥风险**：`~/.claude`、`~/.codex`、`~/.kimi` 等配置含登录凭证，**不能明文进 git**（未来用 chezmoi/age 加密，见 M4）
5. `macsync` 二进制未入库（.gitignore），数据目录 `~/.macsync/` 不入库

---

## 架构

```
┌─ Mac A ──────────────────────┐        ┌─ Mac B ──────────────────────┐
│ Web UI (127.0.0.1:8787)     │        │ Web UI (127.0.0.1:8787)     │
│   └─ Agent (scan/apply/uninstall) │    │   └─ Agent (scan/apply/uninstall) │
└────────────┬─────────────────┘        └────────────┬─────────────────┘
             │ git push/pull（或 Syncthing）            │
             └───────────────┬────────────────────────┘
       manifest.yaml(期望状态,未来) · reports/(快照) · queue.json(待同步)
```

**核心思想：清单 + 快照分离。**
- `reports/<主机名>.json` = 每台机器实际有什么（快照，已实现）
- `queue.json` = 用户勾选的待同步项（已实现）
- `manifest.yaml` = 期望状态、跨机趋同目标（M3 实现）

### 文件结构

```
~/macsync/
├── main.go          CLI 入口：scan / serve / version
├── report.go        报告数据结构（Report/Machine/Brew/UvTools/...）
├── collectors.go    8 类采集器 + findExec/findNodeDirs/runCmd 工具
├── scan.go          collectReport 编排 + emptyIfNil 归一化 + runScan 落盘
├── serve.go         HTTP 服务：静态资源 + API 路由
├── actions.go       同步队列(Queue 持久化) + 卸载动作(runUninstall/trashApp)
├── web/             index.html / app.js / style.css（嵌入二进制）
└── examples/        盘点快照存档（report.snapshot-*.json）
```

### 数据格式

`~/.macsync/current.json`（单机快照核心结构）：

```json
{
  "machine": {"hostname": "...", "os_name": "macOS", "os_version": "...", "chip": "arm64", "shell": "/bin/zsh", "brew_prefix": "/opt/homebrew/bin"},
  "brew": {"formulae": [{"name": "fd", "version": "10.x", "top_level": true, "outdated": false}], "casks": [...], "taps": [...]},
  "uv": {"tools": [{"name": "synto", "version": "0.7.0", "binaries": ["synto"]}]},
  "runtimes": {"rustup_toolchains": [...], "node": {"version": "v20.20.2", "nvm_versions": [...], "global_packages": ["@anthropic-ai/claude-code@2.1.183"]}},
  "cargo": {"bins": [...]},
  "local_bin": {"bins": [...]},
  "apps": [{"name": "Chrome", "path": "/Applications/Chrome.app", "version": "..."}],
  "configs": [{"path": "~/.config/nvim", "type": "dir|file|git-repo", "size": 0, "file_count": 123, "hash": "a1b2c3d4e5f6a7b8", "mod_time": "..."}],
  "generated_at": "..."
}
```

`~/.macsync/queue.json`：

```json
{"items": [{"source": "brew-formula", "name": "fd", "added_at": "2026-08-28T15:44:19+08:00"}]}
```

### API（全部 JSON）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/api/report` | GET 取缓存报告（无数据返回 404+引导）；POST 显式重新盘点并返回 |
| GET/POST | `/api/queue` | GET 取队列；POST `{source,name}` 加入（去重） |
| POST | `/api/queue/remove` | `{source,name}` 移出队列 |
| POST | `/api/uninstall` | `{source,name}` 执行卸载，返回 `{ok, output, error?}` |
| GET/POST | `/api/config` | GET 取同步配置；POST `{github_repo}` 保存 |
| POST | `/api/sync/test` | 测试 GitHub 连接（gh 认证/仓库可达性） |
| POST | `/api/sync` | `{direction:"push"\|"pull"}` 执行同步 |
| GET | `/api/health` | 健康检查 |

---

## 未来计划

### M3 剩余（GitHub 同步 v1 已完成，以下是后续增强）
- **多机 diff 视图**：A↔B 对比（`reports/` 下多份快照），"A 有 fd 而 B 没有" → 一键入 manifest / 入队列
- `manifest.yaml` 期望状态（brew/uv/npm 清单 + 配置文件列表），apply 一键装缺失项
- 同步冲突可视化（本地/远端改动冲突时展示）
- 只同步 leaves（期望状态），依赖交给 brew 自己解析（UI 已为此分层）

### M2 搜索（暂未开工；场景=发现自己没有的工具）
- **数据源已定案**（与 Homebrew 官方 GUI [BrewUI](https://github.com/Homebrew/BrewUI) 保持一致，保证信息对齐）：
  - Homebrew：`formulae.brew.sh/api/formula.json` + `cask.json`（描述/版本/依赖/热度）
  - Python CLI（uv）：PyPI JSON API；npm 全局：`registry.npmjs.org`；通用 CLI：aqua-registry
- 搜到 → 一键「安装到本机」/「加入同步队列」

### 与 Homebrew 官方 GUI（BrewUI）的关系
[BrewUI](https://github.com/Homebrew/BrewUI) 是 Homebrew 官方 macOS GUI（Swift 6 + SwiftUI 原生 App，AGPL-3.0，要求 macOS 26+，`brew install --cask homebrew-app`）。

- **不融合代码**：技术栈不同（原生 App vs Go+Web）；AGPL-3.0 含网络使用条款，引入会强制本项目整体改许可；且它只覆盖 Homebrew 子集
- **协同点**：M2 搜索采用同一套 Homebrew JSON API（信息一致）；需要时可在本项目加「Homebrew 管理」入口（未装则 `brew install --cask homebrew-app`，已装则 `open -a Homebrew`）——注意 BrewUI 无 URL scheme，只能到"打开 App"级别
- **分工**：BrewUI 管 brew 本地安装/升级/诊断（原生体验）；macsync 管跨源盘点（brew+uv+npm+cargo+local-bin+GUI 应用+配置）+ 多机同步 + 离线快照

### M4 打磨
- 配置同步：git 仓库 + 软链（已定决策）；密钥目录用 chezmoi/age 加密
- 定时扫描（launchd 常驻）
- 未信任 tap 在 Web 顶部的醒目警告
- Intel/ARM 架构差异处理（brew 前缀不同）

---

## 决策记录（已确认，勿随意更改）

| 决策点 | 选择 |
|---|---|
| 同步载体 | **git 私有仓库**（版本历史、可回滚、零运维） |
| 技术栈 | **Go 单二进制**（纯标准库，内嵌前端，无外部依赖） |
| 配置同步 | **git 仓库 + 软链**（先快速跑通，遇密钥/模板需求再引入 chezmoi） |
| 卸载语义 | brew/uv/npm 走官方卸载命令；GUI 应用**移废纸篓**而非删除 |
| UI 层次 | brew 区：leaves 主展示、依赖默认折叠 |
| M2 搜索数据源 | **Homebrew JSON API**（`formulae.brew.sh`，与官方 GUI BrewUI 一致）+ PyPI/npm/aqua |
| 与 BrewUI 的关系 | **不融合代码**（技术栈 + AGPL 网络条款），仅数据源对齐与（可选）入口联动 |

---

## 冷启动（从新对话恢复本任务）

如果你（或一个新的 AI 助手对话）需要接着做这个项目：

1. **读本文件**（现在正在读），重点：`当前进度`、`已知问题`、`未来计划`、`决策记录`
2. **项目位置**：`~/macsync`（git 仓库，4 个提交）。文件清单见「文件结构」
3. **验证当前状态**：
   ```bash
   cd ~/macsync
   /opt/homebrew/bin/go build -o macsync .          # 应无错误（Go 1.26，无外部依赖）
   ./macsync scan                                     # 应输出盘点摘要
   curl -s http://127.0.0.1:8787/api/health          # 若服务在跑，返回 {"ok":"true"}
   git log --oneline                                  # 最近提交 efbe32c
   ```
4. **数据位置**：`~/.macsync/`（current.json、reports/、queue.json——不入库，是运行数据）；`examples/` 下有盘点快照存档
5. **用户机器关键事实**（排查时直接用）：
   - macOS 26.6.2 arm64，hostname `PeikedeMac-mini.local`，shell /bin/zsh，brew 前缀 `/opt/homebrew/bin`
   - 26 个 brew leaves / 4 cask / 3 tap；uv 工具：synto 0.7.0、kimi-cli 1.49.0；node 实际走 nvm v20.20.2；claude/codex 是 npm 全局包
   - 配置目录：`~/.config/{nvim,fish,synto,...}`、`~/.claude`、`~/.codex`、`~/.kimi`、`~/.aerospace.toml`（aerospace 已卸载但配置在）
   - 沙箱环境 PATH 极简（`/usr/bin:/bin:/usr/sbin:/sbin`），所有命令要显式用 `/opt/homebrew/bin/xxx`
6. **下一个任务**：按用户要求 **M3 多机同步**（见未来计划）。注意用户搁置了 M2 搜索
7. **沟通风格**：用户用中文交流；重要功能变更先讲清楚再动手；涉及系统级安全操作（卸载、信任 tap）需确认

### 与用户交流的上下文速览

- 项目起因：用户想要"能看到自己装了什么 + 同步到不同 Mac + 同步配置文件"的工具
- 已交付：M0 盘点 + M1 仪表盘 + 卸载/队列交互（4 个提交，全部验证通过）
- 用户当前关注：进度存档、可冷启动（即本 README 的意义）
- 已知遗留：aerospace 已卸载；`nikitabobko/tap` 未信任；沙箱服务不能执行删除类操作（用户需自己起服务）

---

## 许可证

[MIT](LICENSE) © 2026 PEKEW

（第三方工具的使用说明：本项目通过命令行调用 `brew`/`uv`/`npm`/`git`/`gh`，不包含其代码；参考项目 [BrewUI](https://github.com/Homebrew/BrewUI) 为 AGPL-3.0，本项目未使用其源码。）
