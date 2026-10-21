# Docker 镜像发布与维护

## 当前 NAS 离线发布

选优更新已于 2026-10-21 推送到 GitHub 并通过双架构 GHCR 发布检查。现有 amd64 fnOS NAS 仍运行 `dns-assistant:2026.10.20-optimization-r2` 离线镜像，两个服务设置 pull_policy: never。公开镜像发布不会自动更新这台 NAS；升级时应选择验证过的版本，保留密钥和数据卷。

本轮离线镜像包 SHA256 为 `fc77a889e10c6d670ede528fbe10de2994c058123fde86304007bafb14ff8d68`，Docker 导出镜像 ID 为 `sha256:78764719f76186f9622d7cdbc0f71f589ecb3250be7a7533e7da3f6177c910b7`。完整卷备份与现场检查见 [验证记录](validation.md)。后续公开发布时，先通过以下 CI 与镜像检查，再切换 NAS 到对应固定发布版本。

## GHCR 发布流程

镜像地址：`ghcr.io/leoskey/dns-assistant`。工作流位于 [.github/workflows/docker.yml](../.github/workflows/docker.yml)，由 main 推送、`vX.Y.Z` 标签或手动触发；PR 只验证。推送到自己的 fork 时会发布到该 fork 的 GHCR 命名空间，部署时同步修改 `DNS_ASSISTANT_IMAGE`。

## 流程与权限

1. 构建 React、运行 Vitest、Go `-race` 全套测试及 vet。
2. 使用 Buildx 与 QEMU 构建 `linux/amd64`、`linux/arm64`，发布同一个多架构 manifest。
3. 检查 manifest 的两种架构，分别以对应平台运行 `config` 命令，验证发布的二进制可执行；这不等于 ARM NAS 或真实游戏主机验收。
4. Actions 摘要保存镜像 digest；源码标签、revision 与构建来源信息用于追溯。

默认只有 `contents: read`；发布任务额外授予 `packages: write`，登录使用本次运行的 `GITHUB_TOKEN`，不需要 PAT 或 NAS 密钥。第三方 Actions 固定到提交 SHA。PR 不运行发布任务，也不登录 registry。

main 的新运行会取消同一分支的旧运行，版本标签的发布不被该策略取消。若发布后的校验失败，该次运行标记失败，先修复并重新发布，通过前不要据此升级 NAS。

## 标签与版本

程序版本保存在 `internal/app/VERSION`，使用三段数字（例如 `0.1.0`）：功能兼容修复递增补丁号，新增兼容功能递增次版本，不兼容变更递增主版本。规则表版本和数据库 schema 版本独立维护。

升级程序版本时，同步执行 `npm --prefix web version 新版本号 --no-git-tag-version` 更新前端包与锁文件，并修改 README 的当前源码版本。CI 检查它们与 VERSION 一致，并拒绝与程序版本不一致的 Git 标签。版本在管理页“设置”、`GET /api/v1/status` 的 `app_version` 和 `dns-assistant version` 中可查；发布镜像还注入源码提交号，并将相同版本写入 OCI 的 `org.opencontainers.image.version` 标签。

- main：`latest` 和 `sha-完整提交号`。latest 跟随经过测试的 main。
- `vX.Y.Z`：`X.Y.Z`、`X.Y` 和提交号标签；版本发布不移动 latest。次版本标签会指向该系列最近一次发布。
- 手动运行：按所选分支／标签生成标签；仅 main 生成 latest。

发布版本示例：

```sh
git tag -a v0.1.0 -m "DNS Assistant v0.1.0"
git push origin v0.1.0
```

版本与提交号标签用于定位发布。需要保证内容固定时使用 Actions 摘要中的完整 digest；标签可以被有权限的维护者重新发布。

使用首个版本镜像时，设置 `DNS_ASSISTANT_IMAGE=ghcr.io/leoskey/dns-assistant:0.1.0`。版本发布仍需通过两种架构的配置与版本命令检查；已发布版本出现修复时递增版本号，不覆盖旧版本标签。

## GHCR 首次公开

GitHub 仓库公开不代表新创建的 GHCR 包自动公开。首次发布后，在 [包页面](https://github.com/users/leoskey/packages/container/package/dns-assistant) 打开 Package settings，将 Change package visibility 设为 Public。包公开后可匿名拉取，后续发布沿用包可见性。

本项目已于 2026-10-20 设为 Public，amd64、arm64 与 latest 已通过空凭据拉取验证；使用本项目公开镜像时无需重复设置。

```sh
docker buildx imagetools inspect ghcr.io/leoskey/dns-assistant:latest
docker pull ghcr.io/leoskey/dns-assistant:latest
```

检查平台列表包含 `linux/amd64` 与 `linux/arm64`；构建来源证明可能增加 `unknown/unknown` 条目，不属于额外运行架构。无凭据拉取成功才算匿名部署验证通过。

## NAS 拉取加速与升级

中国大陆家宽访问 GHCR 缓慢时，可使用 [南京大学镜像站](https://mirror.nju.edu.cn/) 提供的 GHCR 缓存。发布仍在 ghcr.io；拉取路径改为 `ghcr.nju.edu.cn`，其后仓库路径和标签保持一致。

在 `.env` 设置：

```dotenv
DNS_ASSISTANT_IMAGE=ghcr.nju.edu.cn/leoskey/dns-assistant:latest
```

然后在 NAS 上直接执行：

```sh
docker compose pull
docker compose up -d --wait
```

已部署的 fnOS 项目如果使用 `settings.env` 和直接写在 YAML 内的镜像地址，将两个服务设为：

```yaml
image: ghcr.nju.edu.cn/leoskey/dns-assistant:latest
pull_policy: always
```

在 Docker → 本地镜像 → 添加镜像 → 从 URL 添加中预先拉取该地址，等待完成；随后在 Compose → dns-assistant 中停止本项目，再从“更多 → 构建”重建。pull_policy 确保手动构建时检查镜像更新，构建会恢复两个服务。保留原 settings.env、项目名及两个数据卷。停止本项目至恢复期间会有短暂 DNS 中断；提前完成镜像下载可缩短这一窗口。

fnOS 1.2.0505 的“从 URL 添加”对 `@sha256:...` 输入会错误拆分为仓库与标签；该对话框使用普通版本／提交标签。标准 Docker CLI 与 Compose 可使用完整 digest。需要固定版本或回滚时，保持仓库路径并使用已发布的 `sha-完整提交号`、版本标签，或在 CLI／Compose 使用经核对的 digest。

维护者可用下列命令分别检查上游与缓存的 digest、平台列表；目标发布的两个 digest 应一致。已验证的摘要及测试范围见 [验证记录](validation.md)。

```sh
docker buildx imagetools inspect ghcr.io/leoskey/dns-assistant:latest
docker buildx imagetools inspect ghcr.nju.edu.cn/leoskey/dns-assistant:latest
```

2026-10-20，现有 amd64 fnOS NAS 在 28 秒内完成此前未缓存的约 22.91 MB 测试镜像下载；该镜像未运行并已清理。本项目镜像拉取及两个容器重建在 14 秒内完成，已有层命中 NAS 缓存。两项检查分别证明新层下载路径和本项目升级流程，不将缓存命中速度当作完整冷下载速度。

Docker Hub 的 `registry-mirrors` 不会重定向 GHCR，因此本方案按项目使用明确的 GHCR 缓存地址。需要使用 HTTP 代理时，应配置 Docker daemon 的代理，而非仅给容器添加 HTTP_PROXY；本次未修改 NAS 的全局 Docker 代理或重启 Docker 服务。依据：[Docker daemon 代理](https://docs.docker.com/engine/daemon/proxy/)、[Docker Hub 缓存范围](https://docs.docker.com/docker-hub/image-library/mirror/)。

如果所选拉取入口暂时不可用，已运行容器继续使用本地镜像。可在维护时切回 ghcr.io 对应版本，或从可连通 GHCR 的电脑导出目标架构镜像后导入；记录来源 digest 和文件校验值，继续保留密钥与数据卷。

## NAS 升级与源码构建

按 [部署说明](deployment.md) 操作。默认 `compose.yaml` 拉取 `DNS_ASSISTANT_IMAGE`；`compose.build.yaml` 显式覆盖两个服务为本地构建。两者共享端口、密钥与卷定义。

保留现有项目名称、`.env`／`settings.env` 和两个数据卷。修改镜像后执行 pull 与 up，不使用 `down -v`。固定版本或 digest 后使用相同步骤回滚；数据库格式发生变化时，应依据对应版本迁移说明并保留升级前备份。

依据：[GitHub 发布镜像文档](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)、[GHCR 权限与公开说明](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)、[Docker 多架构构建](https://docs.docker.com/build/ci/github-actions/multi-platform/)。
