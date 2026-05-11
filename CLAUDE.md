# Game Live Room

直播间插件项目 —— 一套 H5 页面合集，包含弹幕/礼物监听服务（Go）、主播后台控制台（H5）、直播间透明前台展示（H5）。

---

## 主 Agent 行为规范

**主 agent 不写代码、不直接修改业务文件。** 所有需求和问题必须分派给对应 agent 处理：

| 场景 | 派给谁 | 判断依据 |
|------|--------|---------|
| 简单、明确的代码改动（改样式、加字段、小功能） | `coder` | 需求清晰，无架构决策 |
| 复杂功能、多文件改动、需要设计方案 | `architect` → `coder` | 涉及架构取舍、前后端协作、数据模型变更 |
| 线上异常、奇怪 bug、性能问题、复杂逻辑分析 | `senior` | 根因不明、需要排查、需要沉淀经验 |
| 部署上线 | `devops` | 代码已就绪，需要推到服务器 |
| 部署后验证 | `tester`（后台） | devops 完成后自动启动 |

**例外**：`CLAUDE.md` 和 `.claude/agents/*.md` 可以直接编辑（配置类文件，不是业务代码）。

---

## 执行规范

| 操作 | Agent | 说明 |
|------|-------|------|
| 编写/修改代码 | `coder` | 不做架构决策，不部署 |
| 部署 | `devops` | 不改业务代码，不分析根因 |
| 排查问题、沉淀经验 | `senior` | 排查完必须执行沉淀判断（两步闸门） |
| 规划多步骤任务 | `architect` | 不写代码，不部署 |
| 部署后线上验证 | `tester` | devops 部署成功后由主 agent 后台启动 |

---

## 上线时间窗口

**直播高峰期（通常 20:00 ~ 次日 02:00）禁止上线**，具体时间以用户直播计划为准。

- 非紧急需求：代码正常开发、提交，积攒到安全窗口统一部署
- 紧急情况（线上故障）：**必须先找用户确认**，获得明确授权后才能上线

---

## Git 安全规范（公开仓库，强制执行）

**本仓库对外公开，以下内容严禁出现在任何 commit 中：**

| 禁止内容 | 说明 | 正确做法 |
|---------|------|---------|
| 直播间 room_id / uid | 主播身份信息 | `ROOM_ID` 环境变量 |
| Cookie / SESSDATA / bili_jct | B站认证凭证 | `BILI_COOKIE` 环境变量 |
| API Key / Secret | 任何第三方密钥 | 对应环境变量 |
| 服务器 IP / 内网地址 | 基础设施信息 | 配置文件（.gitignore） |
| 数据库连接字符串 | 含用户名密码 | `DB_DSN` 环境变量 |
| 用户真实数据 | 弹幕用户 uid、昵称等 | 不进代码，只进 DB |

**强制措施**：
- 所有敏感配置通过环境变量或 `.env` 文件注入，`.env` 必须在 `.gitignore`
- 提供 `.env.example` 作为配置模板（只含 key 名，不含真实值）
- 提供 `config.example.yaml`，真实 `config.yaml` 加入 `.gitignore`
- devops 每次 commit 前必须运行安全检查脚本

---

## 系统架构

### 进程架构

**单一 binary**，包含所有功能：

```
┌─────────────────────────────────────────────────────────────────┐
│                   game-live-room (单进程)                        │
│                                                                  │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────────────┐ │
│  │  B站 WSS     │   │  游戏引擎    │   │  HTTP Server         │ │
│  │  弹幕监听    │──▶│  gift/sc     │──▶│  admin H5 页面       │ │
│  │  礼物/SC     │   │  quiz/vote   │   │  overlay H5 页面     │ │
│  └──────────────┘   └──────┬───────┘   │  REST API            │ │
│                             │           │  WebSocket Hub       │ │
│                             ▼           └──────────────────────┘ │
│                      ┌──────────┐                                │
│                      │  MySQL   │                                │
│                      └──────────┘                                │
└─────────────────────────────────────────────────────────────────┘
```

- **弹幕监听**：连接 B站 WSS，解析事件，驱动游戏引擎
- **游戏引擎**：处理游戏逻辑，更新状态，持久化到 MySQL
- **WebSocket Hub**：广播游戏状态变更到所有连接的 admin/overlay 客户端
- **HTTP Server**：提供静态页面（admin/overlay）+ REST API（游戏配置/控制）

### 技术栈

- **后端（server）**：Go，gorilla/websocket，GORM + MySQL，viper 配置
- **弹幕监听**：B站 WSS 客户端，protover=3（brotli 压缩），支持 DANMU_MSG / SEND_GIFT / SUPER_CHAT_MESSAGE / GUARD_BUY，有指数退避重连逻辑
- **前端（admin + overlay）**：原生 H5 + 轻量 JS（无重型框架，overlay 要求极低延迟）
- **通信**：WebSocket（server ↔ admin ↔ overlay 实时同步，延迟 < 100ms）
- **配置**：viper 读取 `config.yaml`，敏感值通过环境变量覆盖
- **容器化**：Docker Compose（server + MySQL），支持 WSL/Linux 部署

### 目录结构（规划）

```
game-live-room/
├── cmd/server/          # Go 服务入口
├── internal/
│   ├── bilibili/        # 弹幕 WSS 客户端
│   ├── config/          # viper 配置
│   ├── game/            # 游戏引擎（各游戏逻辑）
│   │   ├── quiz/        # 答题游戏（图/题/音频题库）
│   │   ├── vote/        # 礼物投票（麦位送礼加分）
│   │   ├── gift/        # 礼物触发事件
│   │   └── sc/          # SC 触发事件
│   ├── hub/             # WebSocket Hub（广播到 admin/overlay）
│   ├── model/           # 数据模型
│   └── store/           # MySQL 数据访问层
├── web/
│   ├── admin/           # 主播后台 H5
│   └── overlay/         # 直播间前台 H5（透明背景）
│       ├── modules/     # 各展示模块（计分板、弹幕墙、答题、礼物动画等）
│       └── index.html   # 模块容器（OBS 浏览器源入口）
├── migrations/          # MySQL 迁移脚本
├── config.example.yaml  # 配置模板（无敏感值）
├── .env.example         # 环境变量模板
├── docker-compose.yml   # 容器编排
└── Makefile
```

### 数据流

```
B站 WSS
  → DanmakuClient（Go）
  → GameEngine（分发到对应游戏逻辑）
  → MySQL（持久化）
  → WebSocket Hub（广播）
    → admin（实时弹幕流 + 游戏状态）
    → overlay（游戏画面更新）

admin 操作
  → HTTP API（Go）
  → GameEngine（切换游戏/触发事件/修改配置）
  → WebSocket Hub（广播状态变更）
    → overlay（自动切换页面/更新显示）
```

---

## 游戏模块

### 1. 礼物触发事件（GiftTrigger）
- 配置：礼物名 → 触发动作（播放动画、显示文字、音效）
- overlay 模块：全屏礼物动画

### 2. SC 触发事件（SCTrigger）
- 配置：SC 金额阈值 → 触发动作
- overlay 模块：SC 展示卡片

### 3. 答题游戏（Quiz）
- 题库：图片题、文字题、音频题（存 MySQL，admin 管理）
- 玩法：弹幕答题，计时，统计正确率
- overlay 模块：题目展示 + 倒计时 + 答题结果

### 4. 礼物投票（GiftVote）
- 配置：麦位列表 + 对应礼物名 → 加分规则
- 玩法：观众给指定麦位送指定礼物，累计分数
- overlay 模块：实时计分板

---

## Overlay 展示模块（OBS 浏览器源）

overlay 是多模块叠加架构，每个模块独立控制显示/隐藏：

| 模块 | 说明 |
|------|------|
| `danmaku-wall` | 弹幕墙（滚动弹幕展示） |
| `gift-alert` | 礼物/SC 动画提醒 |
| `scoreboard` | 礼物投票计分板 |
| `quiz-display` | 答题题目 + 倒计时 |
| `quiz-result` | 答题结果统计 |
| `online-count` | 在线人数 |
| `combo-counter` | 连击计数（弹幕连发） |

**关键约束**：
- 所有模块背景必须透明（`background: transparent`）
- 动画用 CSS transform/opacity，避免重排
- WebSocket 断线自动重连，重连期间模块保持最后状态

---

## 数据库

MySQL 通过 Docker 运行。

```bash
# 本地开发（Docker）
docker exec -it game_live_room_mysql mysql -u root -p game_live_room

# 查中文内容
docker exec -it game_live_room_mysql mysql -u root -p game_live_room --default-character-set=utf8mb4
```

---

## 开发环境 vs 部署环境

| 维度 | 开发机（Mac/Windows/WSL） | 部署（Linux 服务器） |
|------|--------------------------|------------------|
| 运行方式 | `go run` / `make dev` | Docker Compose |
| 配置 | `.env` 本地文件 | 服务器部署目录下的 `config.yaml`（不进 git） |
| MySQL | Docker 本地容器 | Docker Compose 服务（不对外暴露端口） |
| 前端 | 静态文件直接访问 | nginx 容器反代（10020/10021） |
| 热重载 | air（Go）/ 浏览器刷新 | 不需要 |

## 生产部署架构

```
外部访问
  :10010  →  game_live_room_server（Go，API + WebSocket）
  :10020  →  game_live_room_admin（nginx）→ server:8080/admin/
  :10021  →  game_live_room_overlay（nginx）→ server:8080/overlay/

内部网络（不对外）
  mysql:3306  →  game_live_room_mysql
```

- 服务器信息（IP、SSH 用户、部署路径）：存储在本地 memory，不在此文件记录
- 真实配置文件（config.yaml）：只存在服务器部署目录，严禁进 git

---

## 排查经验

> 详细排查记录见 [docs/troubleshooting.md](docs/troubleshooting.md)（待创建）

| 现象 | 关键结论 |
|------|---------|
| B站 WSS 断线 | DanmakuClient 有指数退避重连（5s→60s），重连前刷新 token，无需手动干预 |
| overlay 背景不透明 | 检查 OBS 浏览器源是否勾选"允许透明"；CSS 必须 `background: transparent` 而非白色 |
| admin/overlay 页面 404 | nginx 反代路径末尾斜杠敏感：`proxy_pass http://server:8080/admin/` 必须带尾斜杠 |
| WebSocket 连接失败（nginx 后） | nginx conf 必须设置 `Upgrade` 和 `Connection` header，见 nginx/admin.conf |
| 容器启动顺序问题 | server 依赖 mysql healthcheck，mysql 未就绪时 server 会等待，正常现象 |
| （待补充） | — |
