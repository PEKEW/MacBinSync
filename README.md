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

# 3) 启动 Web 界面（默认 127.0.0.1:8787）
./macsync serve            # 可选 --port N
```

停止服务：`pkill -f "macsync serve"`。

⚠️ **运行须知（重要）**：如果服务是由受限环境（如 AI 助手的沙箱）启动的，**「卸载/移入废纸篓」等删除操作会失败**（报 `Operation not permitted`，因为要删 `/opt/homebrew`、`/Applications` 下的文件）。**正常使用请从自己的终端运行 `./macsync serve`**，盘点、队列、同步等功能不受影响。

---

## 当前进度（2026-08-28 存档）

### ✅ 已完成：M0 盘点 CLI
`macsync scan` 采集 8 类数据，输出 JSON 报告：

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
| GET/POST | `/api/report` | GET 取当前报告；POST 重新盘点并返回 |
| GET/POST | `/api/queue` | GET 取队列；POST `{source,name}` 加入（去重） |
| POST | `/api/queue/remove` | `{source,name}` 移出队列 |
| POST | `/api/uninstall` | `{source,name}` 执行卸载，返回 `{ok, output, error?}` |
| GET | `/api/health` | 健康检查 |

---

## 未来计划

### M3 多机同步（下一个里程碑）
队列已经就位，M3 直接消费：
- `manifest.yaml` 期望状态（brew formulae/casks/taps、uv tools、npm globals、配置文件列表）
- **多机 diff 视图**：A↔B 对比（`reports/` 下多份快照），"A 有 fd 而 B 没有" → 一键入 manifest / 入队列
- **push/pull/apply 按钮**：push=本机清单推上 git；pull=拉取；apply=按 manifest 装缺失项
- 载体：**git 私有仓库**（已定决策）；版本历史即操作日志，可回滚
- 只同步 leaves（期望状态），依赖交给 brew 自己解析（UI 已为此分层）

### M2 搜索（已搁置，用户要求先做交互；场景=发现自己没有的工具）
- 数据源：Homebrew API（`formulae.brew.sh/api/formula.json` + `cask.json`）、PyPI（uv）、npm registry、aqua-registry、GitHub Releases
- 搜到 → 一键「安装到本机」/「加入同步队列」

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
