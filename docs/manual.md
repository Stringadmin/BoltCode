# BoltCode 使用手册

> 内置 AI 编程 Agent + 可选团队协作层。默认单人开箱即用，`team init` 一键升级到多人×多AI 协作。

---

## 目录

- [BoltCode 是什么](#boltcode-是什么)
- [安装](#安装)
- [快速开始（Solo 模式）](#快速开始solo-模式)
- [AI Agent 会话](#ai-agent-会话)
- [斜杠命令](#斜杠命令)
- [`/triage`：让 AI 审查代码库](#triage让-ai-审查代码库)
- [升级到团队协作（Team 模式）](#升级到团队协作team-模式)
- [连接 GitHub / GitLab 等托管平台](#连接-github--gitlab-等托管平台)
- [自托管服务器](#自托管服务器)
- [命令参考](#命令参考)
- [配置与目录结构](#配置与目录结构)
- [工作流与状态流转](#工作流与状态流转)
- [设计原理](#设计原理)
- [常见问题](#常见问题)

---

## BoltCode 是什么

BoltCode 把一个**内置 AI 编程 Agent** 和一个**可选的团队协作层**装进一个 CLI：

- **Solo 模式（默认）**：一个人 + 一个 AI Agent。开终端、启动会话、让 AI 写代码、你来审查提交。零协作配置。
- **Team 模式**：团队里每个人指挥自己的 AI Agent 并行开发，通过 git-native 的提案审查流程合并成可交付产品。AI 永远不能自己 merge，必须人类审查。

| 模式 | 定位 | 核心命令 |
|------|------|----------|
| **Solo** | 1人 + 1AI，开箱即用 | `init` / `run` / `config` / `push` |
| **Team** | 多人 × 多AI 协作 | `team init` 升级，解锁 `task` / `review` / `sync` |

**核心红线**：AI 永远不直接 merge 或 push。所有合并都是人类显式批准。

---

## 安装

### 前置要求

- **Go 1.26+**（构建用）
- **Git**（已安装并在 PATH 中）
- 一个 **Anthropic API Key**（Claude），或兼容的 OpenAI/Anthropic 网关地址

### 从源码构建

```bash
git clone <你的 boltcode 仓库>
cd BoltCode
go build -o boltcode .
# 可选：安装到 PATH
go install .
```

构建产物是单个二进制 `boltcode`（Windows 上是 `boltcode.exe`），无运行时依赖。

### 验证

```bash
boltcode --help
```

看到 Logo 和帮助说明即安装成功。

---

## 快速开始（Solo 模式）

三步上手，无需任何团队配置。

### 1. 在你的项目里初始化

```bash
cd your-project       # 必须是一个 git 仓库
boltcode init
```

`init` 会：
- 创建 `.boltcode/` 元数据目录
- 记录默认 base 分支和你的身份（从 `git config user.name` 自动获取，按回车即用）
- 默认设为 **solo 模式**

### 2. 配置 AI Agent 的 API Key

进入交互式会话后配置（Key 存在用户级 `~/.boltcode/agent.json`，不会进 git）：

```bash
boltcode          # 进入交互式 shell
> config          # 管理服务商、API Key 和模型档案
```

`config` 支持**多服务商档案**：你可以保存多份配置（如 `anthropic` / `火山方舟` / `local`），每份含独立的 provider、API Key、模型、base_url。向导里：

- 已有档案、服务商和内置模型均可按编号选择，也可以直接输入自定义 provider / 模型 ID
- 切换服务商时会自动带出推荐模型与默认 Base URL；输入 `-` 可恢复服务商默认地址
- API Key 使用隐藏输入，不会显示在终端或写入命令历史
- 每个字段可回车保留 / `b` 返回上一步 / `q` 取消；确认页可按 `1`–`4` 直接返回对应字段
- 保存的档案自动设为当前

> 也可以复用 Claude Code 的配置：若 `~/.boltcode/agent.json` 不存在，BoltCode 会自动读 `~/.claude/settings.json` 里的 `ANTHROPIC_API_KEY` / `ANTHROPIC_BASE_URL`。零额外配置即可用。

#### 切换服务商 / 模型

Agent 会话中：

```
> /profile              # 列出所有档案，标记当前
> /profile 火山方舟      # 切换到该档案（key+model+base_url 一起切，立即生效）
> /model                # 查看当前模型
> /model glm-5.2        # 只切模型（持久化到当前档案）
```

### 3. 启动 Agent 编程

```bash
> run             # solo 模式：直接在当前仓库根目录启动 Agent
```

Agent 会扫描项目、加载跨会话记忆、然后用对话方式跟你协作写代码。详见下节。

### 恢复历史会话（--resume）

每次 agent 会话结束（输入 `exit`/`done`/`quit`）时，BoltCode 会**自动保存**对话到 `~/.boltcode/sessions/<id>.json`，并打印恢复 ID：

```
  ✓ session saved — resume with: boltcode --resume 5521faf4
```

之后随时延续上次对话：

```bash
boltcode --resume 5521faf4   # 恢复指定会话，接着聊
boltcode --resume            # 不带 id：列出最近会话，交互选择
```

- 会话存用户级（跨项目共享），带原 workdir，恢复时自动回到原项目目录上下文
- 恢复后会显示 `↻ resumed session <id> · N messages`，从上次中断处继续
- 会话内可用 `/sessions` 查看所有历史会话
- resume-of-resume 复用同一 ID，保持会话线程连续
- 历史会话默认最多保留 500 条且保留 180 天；可用 `BOLTCODE_SESSION_MAX` 和 `BOLTCODE_SESSION_RETENTION_DAYS` 调整，设为 `0` 可分别关闭数量或时间清理

---

## AI Agent 会话

`run` 启动后进入 Agent 会话。Agent 会：

1. **扫描项目**——文件树、配置文件（go.mod / package.json / Cargo.toml 等）、项目 spec
2. **加载记忆**——从 `.boltcode/local/memory.json` 读取过去会话积累的项目知识
3. **进入对话**——你描述需求，Agent 用内置工具（读写文件、搜索、跑命令）实现，每一步实时显示

### 内置工具

| 工具 | 作用 |
|------|------|
| `read_file` | 读文件（路径限制在 worktree 内，防穿越） |
| `write_file` | 创建/覆盖文件 |
| `edit_file` | 精确替换（`old_string` 必须唯一） |
| `multi_edit` | 对同一文件应用多处编辑（原子：全成或全不成） |
| `search_code` | 正则搜索代码 |
| `run_command` | 执行 shell 命令（需确认） |
| `list_files` | 列目录 |
| `todo_write` | 创建/更新任务计划（长任务拆步骤，防迷失） |
| `update_memory` | 持久化项目知识到跨会话记忆 |

### 上下文管理

长对话不会撑爆模型的上下文窗口（256k/1M tokens）：

- **实时预览**——每轮 AI 回复后的状态行显示上下文进度条：
  ```
  [████████░░░░░░░░] 50% · 64k/128k tokens
  ```
  绿色（健康）→ 黄色（80% 将自动压缩）→ 红色（90% 接近上限）
- **自动压缩**——达到 80% 时自动把旧对话摘要成一段（调 LLM 生成摘要），保留最近若干轮原文，无缝继续
- **手动压缩**——`/compact` 随时触发 LLM 摘要压缩
- **查看用量**——`/context` 显示当前上下文用量 vs 模型窗口
- 窗口大小按当前 profile 的 model 自动设定（claude-sonnet 200k、glm-5 128k 等）

### 工具确认

Agent 执行 shell 命令（构建/测试/git 操作）前会征求你同意：

```
  ⚠ run: go build ./...
  [y/n/a] > 
```

- `y` — 执行这一次
- `n` — 拒绝
- `a` — 本次会话内全部自动允许（`AutoAllow`）

### 会话结束

Agent 完成后，若有未提交改动，会询问是否提交：

```
  commit changes? [Y/n/custom message] >
```

直接回车用默认提交信息，或输入自定义信息，或 `n` 跳过。

### Agent 的内置工具

| 工具 | 作用 |
|------|------|
| `read_file` | 读文件（路径限制在 worktree 内，防穿越） |
| `write_file` | 创建/覆盖文件 |
| `edit_file` | 精确替换（要求 `old_string` 唯一，否则报错让 Agent 补上下文） |
| `search_code` | 正则搜索代码（grep 语义） |
| `run_command` | 执行 shell 命令（需确认） |
| `list_files` | 列目录（跳过 .git/node_modules） |
| `update_memory` | 持久化项目知识到跨会话记忆 |

---

## 斜杠命令

Agent 会话中可输入 `/` 命令：

| 命令 | 作用 |
|------|------|
| `/help` | 显示所有斜杠命令 |
| `/tokens` | 显示 token 用量 |
| `/time` | 显示会话耗时 |
| `/model` | 查看当前模型 |
| `/model <id>` | 切换模型（持久化到当前档案） |
| `/profile` | 列出所有服务商档案 |
| `/profile <name>` | 切换激活的档案 |
| `/diff` | 查看 worktree 未提交改动 |
| `/files` | 列出 worktree 文件 |
| `/clear` | 清空对话历史（保留初始上下文） |
| `/compact` | 压缩上下文（LLM 摘要旧消息） |
| `/context` | 查看上下文用量进度条 |
| `/triage [worktree\|repo]` | 让 AI 审查代码库，给出优先处理项 |
| `/sessions` | 列出历史会话（用 `boltcode --resume <id>` 恢复） |
| `exit` / `done` / `quit` | 结束会话 |

---

## `/triage`：让 AI 审查代码库

输入 `/triage`，Agent 会扫描代码库并基于扫描结果给出**按优先级排序的处理项**。每项包含：分类标签、问题描述（具体到文件/符号）、为什么重要、修复建议。

分类标签：`[bug]` `[security]` `[architecture]` `[tests]` `[tech-debt]` `[perf]`

### 两个作用域

```
> /triage              # 默认：审查当前 worktree
> /triage worktree     # 同上
> /triage repo         # 审查整个主仓库（更全局的视角）
```

| 作用域 | 扫描范围 | Agent 工具访问 |
|--------|----------|----------------|
| `worktree` | Agent 当前工作区 | 完整（可继续深入交互分析） |
| `repo` | 主仓库根目录 | 仅基于扫描的一次性分析（工具受 worktree 限制） |

**说明**：`repo` 作用域给 Agent 全局视野，但因 Agent 的文件工具被限制在 worktree 内，它无法读取主仓库任意文件——这是基于扫描快照的分析。要交互式深挖，用 `worktree` 作用域，或在 solo 模式下（worktree 即仓库根，无此限制）。

triage 结果进入对话历史，你可以直接追问："详细说说第 2 项怎么修"。

---

## 升级到团队协作（Team 模式）

Solo 模式跑顺后，需要多人并行开发时升级：

```bash
boltcode team init [team-name]
```

这会把 config 的 `mode` 改为 `team`，解锁协作命令。升级后：

### 典型 Team 工作流

```bash
# 1. 创建任务（自动建独立 worktree + 分支）
boltcode task new "实现用户登录"

# 2. 启动 Agent 在该任务的隔离 worktree 里工作
boltcode
> run t_a1b2c3d4        # 用任务 id

# 3. Agent 完成后，提交工作作为提案
boltcode submit t_a1b2c3d4 "添加 JWT 登录 + 单元测试"

# 4. 团队成员审查
boltcode review          # 列出待审查提案
boltcode diff p_xxxx     # 看某个提案的 diff

# 5. 审查通过则合并（AI 永远不能做这步）
boltcode merge p_xxxx    # 批准 + 合并到 base 分支
# 或退回让 AI 修改
boltcode reject p_xxxx "密码强度校验缺失，补上"
```

### Team 专属命令

| 命令 | 作用 |
|------|------|
| `task new <intent>` | 创建任务 + 隔离 worktree |
| `task ls` | 列出所有任务 |
| `task claim <id>` | 认领任务（防重复工作） |
| `submit <task> <summary>` | 提交工作为提案 |
| `review` | 列出待审查提案 |
| `diff <proposal>` | 看提案 diff |
| `approve <proposal>` | 批准（不合并） |
| `merge <proposal>` | 批准 + 合并 |
| `reject <proposal> <note>` | 退回并附注 |
| `status` | 团队仪表板 |
| `sync` | 提交 + push/pull 元数据与团队同步 |

### `task new` 参数

```bash
boltcode task new "重构认证模块" \
  --base develop \           # 从哪个分支 fork（默认当前分支）
  --depends t_xxx,t_yyy \    # 依赖哪些任务先完成
  --priority 2 \             # 优先级（数字大更紧急）
  --tags backend,auth        # 标签
```

### 任务依赖

`--depends` 指定的任务未合并/未放弃前，`run` 该任务会被拦截并提示阻塞项。

### 团队同步（`sync`）

```bash
boltcode sync
```

把 `.boltcode/` 下的共享元数据（tasks/proposals/reviews/spec/config）提交并 push/pull。这是团队共享状态的唯一方式——没有服务端，全靠 git。同步后会自动检测活跃任务间的文件冲突风险并告警。

> **Solo 模式下**，上述 team 命令会被门禁拦截，提示 `run 'boltcode team init' to enable`。

---

## 连接 GitHub / GitLab 等托管平台

BoltCode 用**纯 git 协议**连接托管平台，不调用任何平台 API——所以兼容 GitHub / GitLab / Gitea / Bitbucket 等一切支持 git 的服务。

### 添加远程

```bash
# SSH 方式
boltcode remote add git@github.com:yourname/yourrepo.git

# HTTPS 方式
boltcode remote add https://github.com/yourname/yourrepo.git

# GitLab
boltcode remote add git@gitlab.com:yourname/yourrepo.git

# 自定义远程名
boltcode remote add https://github.com/u/r.git --name upstream
```

### 配置认证

托管平台的仓库**已经存在**，无需初始化裸仓库。只需配好认证：

```bash
boltcode remote setup
```

向导会自动检测是托管平台还是自托管服务器：
- **HTTPS**：首次 push 时 git 会问用户名 + Personal Access Token（当密码用）
- **SSH**：向导生成/找到 SSH key，打印公钥让你贴到平台账户（GitHub → Settings → SSH and GPG keys）

### 推送 / 拉取

```bash
boltcode push          # 推送当前分支
boltcode push --force  # 强推（--force-with-lease，更安全）
boltcode pull          # 拉取
```

> **注意**：如果你的全局 git 配置了 `url.<x>.insteadOf` 重写规则（例如把 `git@github.com:` 自动改成 `https://github.com/`），`git remote -v` 显示的可能是重写后的 URL。BoltCode 存的是你输入的原始 URL（`boltcode remote show` 可见），不受影响。

---

## 自托管服务器

不想用托管平台？BoltCode 内置自托管 git 服务器的引导配置（通过 SSH）：

```bash
boltcode
> setup           # 交互式向导，选"自托管"
```

向导会问服务器地址、SSH 端口、用户名、密码、仓库路径，然后：
1. SSH 连接服务器
2. 生成 SSH key 并配置免密登录
3. 在服务器上初始化裸仓库（`git init --bare`）
4. 配置本地 git remote

整个过程不离开终端。配置完即可 `push` / `pull`。

> 自托管服务器路径会做 shell 元字符校验，防止注入。

---

## 命令参考

### 交互式会话命令（`boltcode` 无参进入）

| 命令 | 说明 | 模式 |
|------|------|------|
| `init` | 初始化项目 | 通用 |
| `run [instruction]` | solo 下在仓库根启动 Agent | solo |
| `run <task-id>` | team 下在任务 worktree 启动 Agent | team |
| `config` | 管理服务商 / API Key / 模型档案 | 通用 |
| `team init [name]` | 升级到 team 模式 | solo→team |
| `setup` | 配置远程（托管/自托管） | 通用 |
| `spec` / `spec init` | 查看 / 创建项目 spec | 通用 |
| `push` / `pull` | 推送 / 拉取 | 通用 |
| `task new/ls/claim` | 任务管理 | team |
| `submit` / `review` / `diff` | 提案审查 | team |
| `approve` / `merge` / `reject` | 审查决策 | team |
| `status` | 团队仪表板 | team |
| `sync` | 团队元数据同步 | team |
| `help` / `exit` | 帮助 / 退出 | 通用 |

### 一次性 CLI 子命令

```bash
boltcode init
boltcode team init [team-name]
boltcode remote add <url> [--name <remote>]
boltcode remote setup | show
boltcode push [--force] | pull
boltcode spec init | show | edit
boltcode tui                          # 交互式审查界面
# team 模式下还可：
boltcode task new|ls|claim ...
boltcode submit <task> <summary>
boltcode review | diff <proposal>
boltcode approve|merge|reject <proposal>
boltcode status | sync
```

### `task new` 标志

| 标志 | 作用 | 默认 |
|------|------|------|
| `--base <branch>` | fork 起始分支 | 当前分支 |
| `--depends <ids>` | 依赖的任务 id（逗号分隔） | 无 |
| `--priority N` | 优先级 | 0 |
| `--tags a,b` | 标签 | 无 |

> `run`、`config` 和所有斜杠命令只在交互式会话内可用，不是 CLI 子命令。

---

## 配置与目录结构

### 项目级 `.boltcode/`

```
.boltcode/                    ← 推送到远程（团队共享）
├── config.json              ← {default_base, user, team, mode}
├── remote.json              ← 远程配置（URL + remote 名）
├── spec.md                  ← 项目规格（AI 必须遵守的"宪法"）
├── .gitignore               ← 含 "local/"
├── tasks/                   ← 任务 JSON（每任务一文件）
├── proposals/               ← 提案 JSON
├── reviews/                 ← 审查记录
└── local/                   ← 不推送（本机私有）
    ├── memory.json          ← Agent 跨会话记忆
    └── worktrees/           ← 每个 task 的隔离工作区
```

### 用户级 `~/.boltcode/`

```
~/.boltcode/
├── agent.json               ← 服务商档案（0600 权限，不上传）
├── sessions/                ← 自动保存的 agent 会话（每会话一文件）
└── history                  ← 交互式 shell 历史
```

`agent.json` 存多份命名档案 + 当前指针：

```json
{
  "profiles": [
    {"name": "anthropic", "provider": "anthropic", "api_key": "sk-...", "model": "claude-sonnet-4-20250514"},
    {"name": "火山方舟", "provider": "volc", "api_key": "sk-...", "model": "glm-5.2", "base_url": "https://ark.cn-beijing.volces.com/api/v3"}
  ],
  "current": "火山方舟"
}
```

> 旧版单配置文件（只有 `api_key` 等扁平字段）会自动迁移成一个名为 `default` 的档案，向后兼容。

### `config.json` 字段

| 字段 | 说明 |
|------|------|
| `default_base` | 任务 fork / 合并的目标分支 |
| `user` | 当前开发者身份 |
| `team` | 团队名（team 模式，可选） |
| `mode` | `"solo"`（默认）或 `"team"` |

> 旧项目无 `mode` 字段会被视为 solo，向后兼容，无需迁移。

### `remote.json` 字段

| 字段 | 说明 |
|------|------|
| `url` | 远程 URL（支持 github/gitlab 等任意 git URL） |
| `name` | git remote 名，默认 `origin` |

---

## 工作流与状态流转

### 任务状态机

```
open ──run──▶ in_progress ──submit──▶ review ──merge──▶ merged
                  ▲                       │
                  │                     reject
                  └───────────────────────┘
                          (回到 open 供 AI 修改)

任意状态 ──▶ abandoned（放弃）
```

### 提案状态

```
pending ──approve──▶ approved ──merge──▶ （任务变 merged）
   │
   └──reject──▶ （任务回 open）
```

**审查状态机守卫**：
- 已 `rejected` 的提案不能直接 `approve`（需重新 `submit`）
- 已 `merged` 的任务不能 `approve` / `reject`
- `approve` / `reject` 对同状态幂等

### 冲突检测

`sync` 后会检测所有 `in_progress` / `review` 状态任务的 `files_changed` 重叠，路径按归一化（分隔符/大小写不敏感）比对，有重叠则告警"冲突风险"。

---

## 设计原理

1. **共享状态用 git 而非服务端** — 简单可靠，兼容所有 git 托管，无需自建后端
2. **Agent 内置而非依赖外部工具** — 零配置，开箱即用，工具链自洽
3. **Worktree 隔离** — 每个 task 独立工作区，多 Agent 并行互不干扰
4. **Human-in-the-loop** — AI 永远不能 merge/push，所有合并人类显式批准
5. **Solo 优先，Team 可选** — 默认单人零门槛，需要时一键升级协作
6. **纯 git 协议连平台** — 不依赖任何平台 API，兼容一切 git 服务
7. **`.boltcode/` 共享、`.boltcode/local/` 私有** — 协作状态同步，本机状态隔离

### 架构分层

1. **Layer 1 本地层** — `.boltcode/` 文件，单机可用
2. **Layer 2 Git 同步层** — push/pull，多人协作核心
3. **Layer 3 P2P 实时层** — 路线图，锦上添花

---

## 常见问题

### `run 'config' to set up` 报错

没配 API Key。进入交互式会话执行 `config`，或确保 `~/.claude/settings.json` 里有 `ANTHROPIC_API_KEY`（会自动复用）。

### `"task" is a team-collaboration feature` 报错

你在 solo 模式用了 team 命令。要么 `boltcode team init` 升级，要么用 solo 的 `run` 直接编程。

### Agent 会话被 API 错误中断

BoltCode 对 429/5xx/网络错误会自动指数退避重试（最多 4 次，尊重 `Retry-After`）。若仍失败，检查 API Key、网络、或额度。4xx（鉴权/参数错误）不重试，需修正配置。

### `git remote -v` 显示的 URL 和我输入的不一样

你的全局 git 配了 `url.<x>.insteadOf` 重写规则。BoltCode 存的是原始 URL（`boltcode remote show` 可见），功能不受影响。

### `/triage repo` 说 Agent 不能读主仓库文件

这是设计约束：Agent 的文件工具被限制在 worktree 内（防越权）。`repo` 作用域是基于扫描快照的一次性分析。要交互式深挖，用 `/triage worktree`，或在 solo 模式下运行（worktree 即仓库根）。

### 如何降级回 solo 模式

目前需手动编辑 `.boltcode/config.json`，把 `"mode": "team"` 改为 `"mode": "solo"`（或删除该字段）。

### API Key 安全吗

Key 存在用户级 `~/.boltcode/agent.json`，权限 `0600`，**不会**进入项目目录、不会被 git 追踪。

---

## 下一步

- 读完本手册，从 `boltcode init` + `run` 开始 solo 编程
- 用 `/triage` 让 AI 帮你找出项目里该优先处理的项
- 需要协作时 `boltcode team init`，再 `remote add` 连上 GitHub
- 团队成员各自 `boltcode init` + `sync` 即可加入协作

---

*BoltCode v0.1.0 · collaborative AI agent programming*
