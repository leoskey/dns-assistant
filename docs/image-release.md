# Docker 镜像发布与维护

镜像地址：`ghcr.io/leoskey/dns-assistant`。工作流位于 [.github/workflows/docker.yml](../.github/workflows/docker.yml)，由 main 推送、`vX.Y.Z` 标签或手动触发；PR 只验证。推送到自己的 fork 时会发布到该 fork 的 GHCR 命名空间，部署时同步修改 `DNS_ASSISTANT_IMAGE`。

## 流程与权限

1. 构建 React、运行 Vitest、Go `-race` 全套测试及 vet。
2. 使用 Buildx 与 QEMU 构建 `linux/amd64`、`linux/arm64`，发布同一个多架构 manifest。
3. 检查 manifest 的两种架构，分别以对应平台运行 `config` 命令，验证发布的二进制可执行；这不等于 ARM NAS 或真实游戏主机验收。
4. Actions 摘要保存镜像 digest；源码标签、revision 与构建来源信息用于追溯。

默认只有 `contents: read`；发布任务额外授予 `packages: write`，登录使用本次运行的 `GITHUB_TOKEN`，不需要 PAT 或 NAS 密钥。第三方 Actions 固定到提交 SHA。PR 不运行发布任务，也不登录 registry。

main 的新运行会取消同一分支的旧运行，版本标签的发布不被该策略取消。若发布后的校验失败，该次运行标记失败，先修复并重新发布，通过前不要据此升级 NAS。

## 标签与版本

- main：`latest` 和 `sha-完整提交号`。latest 跟随经过测试的 main。
- `vX.Y.Z`：`X.Y.Z`、`X.Y` 和提交号标签；版本发布不移动 latest。次版本标签会指向该系列最近一次发布。
- 手动运行：按所选分支／标签生成标签；仅 main 生成 latest。

发布版本示例：

```sh
git tag v0.1.8
git push origin v0.1.8
```

版本与提交号标签用于定位发布。需要保证内容固定时使用 Actions 摘要中的完整 digest；标签可以被有权限的维护者重新发布。

## GHCR 首次公开

GitHub 仓库公开不代表新创建的 GHCR 包自动公开。首次发布后，在 [包页面](https://github.com/users/leoskey/packages/container/package/dns-assistant) 打开 Package settings，将 Change package visibility 设为 Public。包公开后可匿名拉取，后续发布沿用包可见性。

本项目已于 2026-10-20 设为 Public，amd64、arm64 与 latest 已通过空凭据拉取验证；使用本项目公开镜像时无需重复设置。

```sh
docker buildx imagetools inspect ghcr.io/leoskey/dns-assistant:latest
docker pull ghcr.io/leoskey/dns-assistant:latest
```

检查平台列表包含 `linux/amd64` 与 `linux/arm64`；构建来源证明可能增加 `unknown/unknown` 条目，不属于额外运行架构。无凭据拉取成功才算匿名部署验证通过。

如果 NAS 无法访问 ghcr.io，检查 NAS 的 DNS、网络出口与 registry 连接；不要把令牌或服务密钥放入公开 Compose。已拉取镜像不受后续 registry 暂时不可达影响。

直连拉取过慢时，可在能访问 GHCR 的电脑按目标 NAS 架构拉取指定提交标签，使用 `docker save` 导出，再通过 NAS 镜像导入功能载入。Compose 继续使用同一 GHCR 提交标签，保留原密钥与数据卷。记录来源 digest、目标架构及导出文件校验值；导入成功不等于 NAS 直连 registry 已通过。

## NAS 升级与源码构建

按 [部署说明](deployment.md) 操作。默认 `compose.yaml` 拉取 `DNS_ASSISTANT_IMAGE`；`compose.build.yaml` 显式覆盖两个服务为本地构建。两者共享端口、密钥与卷定义。

保留现有项目名称、`.env`／`settings.env` 和两个数据卷。修改镜像后执行 pull 与 up，不使用 `down -v`。固定版本或 digest 后使用相同步骤回滚；数据库格式发生变化时，应依据对应版本迁移说明并保留升级前备份。

依据：[GitHub 发布镜像文档](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)、[GHCR 权限与公开说明](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)、[Docker 多架构构建](https://docs.docker.com/build/ci/github-actions/multi-platform/)。
