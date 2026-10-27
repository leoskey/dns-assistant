# Docker 镜像发布与维护

## 版本与部署核对

源码版本、已发布镜像与设备运行版本分别核对。源码提交不代表 GHCR 发布成功，镜像发布也不会自动更新已运行的容器。部署前检查目标提交对应的 Actions 结果、镜像版本、revision、digest 与目标架构；重建后通过管理页或 `dns-assistant version` 核对运行版本。

本文描述发布流程，不固定记录 `latest` 的内容或具体设备状态。既有检查范围见 [验证记录](validation.md)，历史 CI 证据见 [重构前验证摘要](history/pre-optimization-validation.md)。

## GHCR 发布流程

镜像地址：`ghcr.io/leoskey/dns-assistant`。工作流位于 [.github/workflows/docker.yml](../.github/workflows/docker.yml)，由 main 推送、`vX.Y.Z` 标签或手动触发；PR 只验证。推送到自己的 fork 时会发布到该 fork 的 GHCR 命名空间，部署时同步修改 `DNS_ASSISTANT_IMAGE`。

## 流程与权限

1. 构建 React、运行 Vitest、Go `-race` 全套测试及 vet。
2. 使用 Buildx 与 QEMU 构建 `linux/amd64`、`linux/arm64`，发布同一个多架构 manifest。
3. 检查 manifest 的两种架构，分别以对应平台运行 `config` 和 `version` 命令，核对二进制可执行、版本与源码提交一致；模拟运行不等于 ARM 服务主机或真实游戏主机验收。
4. Actions 摘要保存镜像 digest；源码标签、revision 与构建来源信息用于追溯。

默认只有 `contents: read`；发布任务额外授予 `packages: write`，登录使用本次运行的 `GITHUB_TOKEN`，不需要 PAT 或服务主机密钥。第三方 Actions 固定到提交 SHA。PR 不运行发布任务，也不登录 registry。

main 的新运行会取消同一分支的旧运行，版本标签的发布不被该策略取消。若发布后的校验失败，该次运行标记失败，先修复并重新发布，通过前不要据此升级服务主机。

## 标签与版本

程序版本保存在 `internal/app/VERSION`，使用三段数字（例如 `0.1.0`）：功能兼容修复递增补丁号，新增兼容功能递增次版本，不兼容变更递增主版本。规则表版本和数据库 schema 版本独立维护。

升级程序版本时，同步执行 `npm --prefix web version 新版本号 --no-git-tag-version` 更新前端包与锁文件，并修改 README 的当前源码版本。CI 检查它们与 VERSION 一致，并拒绝与程序版本不一致的 Git 标签。版本在管理页“设置”、`GET /api/v1/status` 的 `app_version` 和 `dns-assistant version` 中可查；发布镜像还注入源码提交号，并将相同版本写入 OCI 的 `org.opencontainers.image.version` 标签。

- main：`latest` 和 `sha-完整提交号`。latest 由通过测试的 main 发布任务更新；部署前仍须检查发布后的镜像校验结果。
- `vX.Y.Z`：`X.Y.Z`、`X.Y` 和提交号标签；版本发布不移动 latest。次版本标签会指向该系列最近一次发布。
- 手动运行：按所选分支／标签生成标签；仅 main 生成 latest。

发布版本示例：

```sh
git tag -a v0.1.0 -m "DNS Assistant v0.1.0"
git push origin v0.1.0
```

版本与提交号标签用于定位发布。需要保证内容固定时使用 Actions 摘要中的完整 digest；标签可以被有权限的维护者重新发布。

例如，确认 `v0.1.0` 已成功发布后，设置 `DNS_ASSISTANT_IMAGE=ghcr.io/leoskey/dns-assistant:0.1.0`。版本发布须通过两种架构的配置与版本命令检查；已发布版本出现修复时递增版本号，不覆盖旧版本标签。

## GHCR 首次公开

GitHub 仓库公开不代表新创建的 GHCR 包自动公开。首次发布后，在 [包页面](https://github.com/users/leoskey/packages/container/package/dns-assistant) 打开 Package settings，将 Change package visibility 设为 Public。包公开后可匿名拉取，后续发布沿用包可见性。

已公开的包无需重复更改可见性。部署时用空 Docker 凭据目录检查目标镜像的匿名拉取能力；历史成功不保证当前标签仍可用。

```sh
docker buildx imagetools inspect ghcr.io/leoskey/dns-assistant:latest
docker pull ghcr.io/leoskey/dns-assistant:latest
```

检查平台列表包含 `linux/amd64` 与 `linux/arm64`；构建来源证明可能增加 `unknown/unknown` 条目，不属于额外运行架构。无凭据拉取成功才算匿名部署验证通过。

## 镜像拉取与升级

中国大陆家宽访问 GHCR 缓慢时，可检查 [南京大学镜像站](https://mirror.nju.edu.cn/) 的 GHCR 缓存入口是否可用。发布仍在 ghcr.io；缓存拉取路径为 `ghcr.nju.edu.cn`，其后仓库路径和标签保持一致。使用前核对目标版本的可达性、摘要与平台列表，不承诺固定下载速度。

在 `.env` 设置：

```dotenv
DNS_ASSISTANT_IMAGE=ghcr.nju.edu.cn/leoskey/dns-assistant:latest
```

然后在服务主机上直接执行：

```sh
docker compose pull
docker compose up -d --wait
```

自定义 Compose 若直接填写镜像地址，须同步替换两个服务的 image；保留环境文件、`env_file` 引用、项目名称及两个数据卷，并移除阻止网络拉取的 `pull_policy: never`。通过服务主机图形界面维护时，先完成镜像下载，再按该系统的 Compose 重建操作更新服务；重建期间会有短暂 DNS 中断。

部分图形界面可能不支持完整的 `@sha256:...` 地址；遇到解析失败时，可使用已发布的版本／提交标签，或改用 Docker CLI／Compose 指定经核对的 digest。

维护者可用下列命令分别检查上游与缓存的 digest、平台列表；目标发布的两个 digest 应一致。以下使用 latest 演示，实际核对时应替换为同一目标版本；标签移动或缓存滞后时先停止升级，重新核对。

```sh
docker buildx imagetools inspect ghcr.io/leoskey/dns-assistant:latest
docker buildx imagetools inspect ghcr.nju.edu.cn/leoskey/dns-assistant:latest
```

检查下载路径时，分别验证未缓存层的下载和本项目的拉取、重建、数据保留及健康状态。缓存命中的升级耗时不能作为完整冷下载速度，过去的可用性也不能代替目标网络的检查。

Docker Hub 的 `registry-mirrors` 不会重定向 GHCR，因此本方案按项目使用明确的 GHCR 缓存地址。需要使用 HTTP 代理拉取镜像时，应配置 Docker daemon 的代理，而非仅给容器添加 HTTP_PROXY。依据：[Docker daemon 代理](https://docs.docker.com/engine/daemon/proxy/)、[Docker Hub 缓存范围](https://docs.docker.com/docker-hub/image-library/mirror/)。

如果所选拉取入口暂时不可用，已运行容器继续使用本地镜像。可在维护时切回 ghcr.io 对应版本，或从可连通 GHCR 的电脑导出目标架构镜像后导入；记录来源 digest 和文件校验值，继续保留密钥与数据卷。

## 升级与源码构建

按 [部署说明](deployment.md) 操作。单独使用 `compose.yaml` 时拉取 `DNS_ASSISTANT_IMAGE`；README 的源码部署同时使用 `compose.build.yaml`，显式覆盖两个服务为本地构建。两种方式共享端口、密钥与卷定义，维护命令须保持所选方式一致。

保留现有项目名称、环境文件和两个数据卷。镜像部署修改目标版本后执行 pull 与 up；源码部署切换源码后，带上两个 `-f` 参数执行 build 与 up。不使用 `down -v`。回滚到固定镜像或源码版本时保持相应部署方式；数据库格式发生变化时，按迁移说明同时恢复匹配的数据卷快照。

依据：[GitHub 发布镜像文档](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)、[GHCR 权限与公开说明](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)、[Docker 多架构构建](https://docs.docker.com/build/ci/github-actions/multi-platform/)。
