# game-live-room

直播间互动插件系统 —— 为 B 站直播间提供弹幕答题、礼物投票、SC 展示等互动功能，配套主播后台控制台和 OBS 透明前台展示。

## 功能概览

| 模块 | 说明 |
|------|------|
| **弹幕答题 Quiz** | 从题库选题，观众发弹幕作答，计时结束后展示正确率 |
| **礼物投票 Vote** | 观众给指定麦位送礼物累计分数，实时计分板 |
| **礼物触发 Gift** | 指定礼物触发 overlay 动画/提示 |
| **SC 展示** | 超过金额阈值的 SC 自动在 overlay 弹出展示卡片 |
| **弹幕墙** | 实时弹幕滚动展示（OBS 透明叠加层） |

## 架构

```
B站 WSS 弹幕流
  └─▶ DanmakuClient（Go）
        └─▶ GameEngine（quiz / vote / gift / sc）
              ├─▶ MySQL（持久化）
              └─▶ WebSocket Hub
                    ├─▶ Admin 后台（实时弹幕流 + 游戏控制）
                    └─▶ Overlay（OBS 浏览器源，透明背景）
```

单一 Go binary，包含 HTTP 服务、WebSocket Hub、弹幕监听、游戏引擎。

## 快速开始

### 前置要求

- Docker + Docker Compose
- B 站账号 Cookie（SESSDATA / bili_jct / DedeUserID）

### 1. 克隆并配置

```bash
git clone https://github.com/AUPaiDev/game-live-room.git
cd game-live-room

# 复制配置模板
cp config.example.yaml config.yaml
cp .env.example .env
```

编辑 `.env`，填入必要信息：

```env
MYSQL_ROOT_PASSWORD=your_strong_password
BILI_ROOM_ID=your_room_id
BILI_COOKIE=SESSDATA=xxx; bili_jct=xxx; DedeUserID=xxx
```

编辑 `config.yaml`，按需调整游戏参数（答题超时、SC 阈值等）。

### 2. 启动服务

```bash
docker compose --env-file .env up -d --build
```

### 3. 访问

| 地址 | 说明 |
|------|------|
| `http://your-server:10010/admin/` | 主播后台控制台 |
| `http://your-server:10010/overlay/` | OBS 浏览器源地址 |
| `http://your-server:10010/api/health` | 健康检查 |

## 后台控制台

后台采用赛博朋克霓虹风格设计，通过侧边栏图标导航切换功能面板。

### 认证

如果配置了 `ADMIN_TOKEN` 环境变量，访问后台需要在 URL 中携带 token：

```
http://your-server:10010/admin/?token=your_token
```

Token 会自动保存到 `sessionStorage`，刷新后无需重新输入。

### 功能面板

**DASH — 仪表盘**
- 实时弹幕流（含礼物、SC、上舰事件）
- 系统状态（B站连接、WebSocket、当前游戏）
- 快捷操作（一键停止所有游戏）

**QUIZ — 答题**
- 题库管理（新增文字/图片/音频题）
- 选题后一键开始，支持手动提前结束
- 实时显示答题状态

**VOTE — 礼物投票**
- 实时计分板（按得分排序，带进度条）
- 开始/停止/重置投票
- 查看当前麦位配置

**GIFT — 礼物触发**
- 手动触发指定礼物的 overlay 动画

**SC — 超级留言**
- 设置 SC 展示最低金额阈值
- 查看本场 SC 记录

**AUTH — 账号管理**
- 管理多个 B 站账号 Cookie
- 扫码登录（生成二维码，手机扫码后自动保存 Cookie）
- 切换活跃账号

## OBS 配置

1. 在 OBS 添加「浏览器」来源
2. URL 填入 `http://your-server:10010/overlay/`
3. 宽度 `1920`，高度 `1080`
4. 勾选「允许透明背景」
5. 勾选「页面关闭时关闭音频输出」（可选）

Overlay 模块默认隐藏，由后台控制台或游戏引擎自动触发显示/隐藏。

## 题库管理

在后台 QUIZ 面板点击「+ 新增题目」，支持三种题型：

| 类型 | 说明 |
|------|------|
| `text` | 纯文字题 |
| `image` | 图片题（需提供图片路径） |
| `audio` | 音频题（需提供音频路径） |

观众通过弹幕发送 `A/B/C/D` 或 `1/2/3/4` 作答，答题时间由 `config.yaml` 中 `answer_timeout` 控制。

## 礼物投票配置

在 `config.yaml` 中配置麦位和对应礼物：

```yaml
game:
  vote:
    enabled: true
    slots:
      - id: "1"
        name: "1号位"
        gifts: ["辣条", "小心心"]
      - id: "2"
        name: "2号位"
        gifts: ["礼物盒子"]
```

## 环境变量

| 变量 | 说明 | 必填 |
|------|------|------|
| `MYSQL_ROOT_PASSWORD` | MySQL root 密码 | ✅ |
| `BILI_ROOM_ID` | B站直播间 room_id | ✅ |
| `BILI_COOKIE` | B站账号 Cookie | ✅ |
| `ADMIN_TOKEN` | 后台访问 token（空则不鉴权） | ❌ |
| `SERVER_PORT` | 服务端口，默认 8080 | ❌ |

## 常用运维命令

```bash
# 查看服务日志
docker compose logs -f server

# 重启服务
docker compose restart server

# 更新部署
git pull origin main && docker compose --env-file .env up -d --build

# 进入 MySQL
docker exec -it game_live_room_mysql mysql -u root -p game_live_room --default-character-set=utf8mb4

# 健康检查
curl http://localhost:10010/api/health
```

## 安全说明

本仓库为公开仓库，以下内容**严禁**出现在任何 commit 中：

- B站 room_id / Cookie / SESSDATA
- 数据库密码
- 服务器 IP / 内网地址

所有敏感配置通过环境变量或 `.env` 文件注入，`.env` 和 `config.yaml` 已加入 `.gitignore`。

## 技术栈

- **后端**：Go，gorilla/websocket，GORM + MySQL，viper
- **弹幕监听**：B站 WSS（protover=3 brotli 压缩），支持 DANMU_MSG / SEND_GIFT / SUPER_CHAT_MESSAGE / GUARD_BUY，指数退避重连
- **前端**：原生 H5 + JS（无框架），赛博朋克霓虹风格
- **容器化**：Docker Compose
