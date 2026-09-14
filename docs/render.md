# 发布到 Render

当前仓库的 `render.yaml` 是 **免费试用、仅在线看**：`plan: free`，无磁盘，关闭下载/合并，点播用更低码率转码并关掉预缓存。休眠或重新部署后剧库缓存会丢；512MB 上点播仍可能卡或失败。确认能用后再升到 1c-2g 并加 Persistent Disk（挂 `/data`）。


这是私人自用的 Web 部署，不是公开短剧站。Render 会给 `*.onrender.com` 公网地址，没有登录谁都能进。程序在 Render 上**必须**设置 `JUKU_BASIC_AUTH_USER` 和 `JUKU_BASIC_AUTH_PASS`，否则拒绝启动。

只应观看或下载你拥有权利或已获授权的内容。点播会在服务器上 FFmpeg 转码，流量和 CPU 都从 Render 走，不适合当在线影院。

## 不适合的情况

- 免费实例：512MB、无持久盘、闲置休眠，剧库缓存和下载会丢。
- 不设密码的公网服务。
- 把 Render 当视频 CDN 给很多人播。

更省事的替代：家里电脑跑剧库，用 Tailscale 或 Cloudflare Tunnel。见项目 README。

## 准备

1. 把本目录单独建成 Git 仓库并推到 GitHub / GitLab（Render 从 Git 构建）。
2. [注册 Render](https://dashboard.render.com/) 并绑好仓库。
3. 准备一个只有自己知道的登录名；密码可让 Render 生成。

## 方法 A：用 Blueprint（推荐）

README 里的按钮会打开：

[![Deploy to Render](https://render.com/images/deploy-to-render-button.svg)](https://render.com/deploy?repo=https://github.com/linux-jin/guoguo)

或仓库根目录已有 `render.yaml`，也可在 Dashboard 里 New → Blueprint。

1. Render Dashboard → **New** → **Blueprint**。
2. 选这个仓库，应用 Blueprint。
3. 区域选 **Singapore**（已写在 yaml 里），套餐 **1 CPU 2 GB**。
4. 给 `JUKU_BASIC_AUTH_USER` 填用户名。
5. 保存后打开服务的 Environment，复制 `JUKU_BASIC_AUTH_PASS`。
6. 等 Docker 构建完成。健康检查路径是 `/healthz`（不走登录）。
7. 打开 `https://xxx.onrender.com`，浏览器弹出账号密码。

持久盘挂在 `/data`：剧库缓存、任务、下载都在这。换实例或没挂盘，数据会丢。

## 方法 B：Dashboard 手动创建

1. **New** → **Web Service** → 连接仓库。
2. Language 选 **Docker**，Dockerfile Path 为 `./Dockerfile`。
3. Region：**Singapore**。
4. Instance：**Starter 不够**，选 **1 CPU / 2 GB** 或更大。
5. Advanced → **Persistent Disk**：Mount path `/data`，至少 20 GB。
6. Health Check Path：`/healthz`。
7. Environment：

   | Key | Value |
   | --- | --- |
   | `TZ` | `Asia/Shanghai` |
   | `JUKU_FFMPEG` | `ffmpeg` |
   | `JUKU_BASIC_AUTH_USER` | 你的用户名 |
   | `JUKU_BASIC_AUTH_PASS` | 强密码 |

Render 会注入 `PORT`（默认 10000）。镜像入口脚本已经监听 `0.0.0.0:$PORT`。

## 部署后

- 页面有 HTTP Basic 登录；`/healthz` 返回 `ok`。
- 容器里不能用系统文件夹选择器，下载目录固定为 `/data/downloads`。
- 不要把账号发到公开地方。
- 若只想自己用、又有家宽，优先改回 Tunnel，Render 流量费通常更贵。
