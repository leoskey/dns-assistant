# Windows 单机安装

支持 Windows 10／11 x64。安装包与便携版均包含管理页面，无需 Docker、Go、Node.js 或额外数据库。电脑需要固定局域网 IPv4，并在主机使用期间保持运行；退出程序或电脑休眠会停止 DNS。

## 安装与启动

从项目 Release 或构建产物中选择 `dns-assistant-X.Y.Z-windows-amd64-setup.exe`。安装到当前用户目录，无需管理员权限；开始菜单的 **DNS Assistant** 启动服务，**Management** 打开 `http://127.0.0.1:8080`。服务窗口需要保持打开，按 Ctrl+C 正常停止。安装不修改系统 DNS，不添加防火墙规则或开机服务。

便携版使用 `dns-assistant-X.Y.Z-windows-amd64.zip`，解压至可写目录，双击 `dns-assistant.exe`，再打开同一管理地址。便携版默认把数据保存在程序旁的 `data` 目录；安装版开始菜单入口使用 `%LOCALAPPDATA%\DNS Assistant\data`。

游戏主机的 DNS 填写这台电脑的固定局域网 IPv4。要让首页显示这个地址，可修改开始菜单快捷方式，在参数末尾添加 `-ip 192.168.1.20`（替换为服务主机地址），或者在 PowerShell 中运行：

```powershell
.\dns-assistant.exe standalone -ip 192.168.1.20 -data "$env:LOCALAPPDATA\DNS Assistant\data"
```

便携模式不指定 `-data` 时使用便携目录；不设置 `-ip` 时仍可解析，但首页不会猜测 DNS 地址。`SERVER_IP` 环境变量也可提供同一地址。修改快捷方式后重新启动程序生效。

首次运行检查 TCP／UDP 53 与 TCP 8080 无冲突。Windows 防火墙中只允许受信任的专用局域网访问本程序的 UDP／TCP 53 和 TCP 8080；不要允许公共网络或公网转发。程序保留局域网来源、Host 与写操作校验；使用其他主机名访问时设置 `ADMIN_HOSTS`。不要直接关闭防火墙解决访问问题。

## 运行与维护

- DNS 与管理同进程运行，内部通信只监听 `127.0.0.1` 随机端口，服务密钥自动生成，无需手工填写。Docker 继续采用两个独立容器，管理容器停止时 DNS 可继续工作；单机程序退出会同时停止两项服务。
- 数据包含 `controller/assistant.db` 及 DNS 快照 `dns/policy.json`。升级前按 Ctrl+C 停止，备份完整数据目录；不要在程序写数据库时只复制 `.db`。数据放在本地磁盘，不使用 SMB／NFS。
- 安装版升级使用同一安装目录和开始菜单数据路径。卸载保留数据；需要清除时，先停止服务、让游戏主机恢复自动 DNS，再手动删除数据目录。
- 便携版升级将新版本解压至新目录，停止旧版后复制完整 `data`，或始终使用 `-data` 指向独立数据目录。回滚同时恢复与旧版兼容的数据备份。
- `dns-assistant.exe version` 查看版本及源码 revision；`config` 输出默认配置。DNS 存活检查使用 `$env:DNS_HEALTH_ADDR='127.0.0.1:53'; .\dns-assistant.exe health dns`；管理检查使用 `.\dns-assistant.exe health`（默认 8080）。
- 排查端口可使用 `Get-NetTCPConnection -State Listen -LocalPort 53,8080` 与 `Get-NetUDPEndpoint -LocalPort 53`。端口冲突会报告地址及错误，不会结束其他程序。仅环回测试时可用 `standalone -listen 127.0.0.1:5353`；主机接入仍需要标准 53 端口。

安装包暂未代码签名；请核对来源和随包发布的 `SHA256SUMS`。构建和隔离测试不代表防火墙、睡眠唤醒或真实主机下载收益已经验证。

## 从源码打包

维护者需要 Go 1.27.2、Node.js 24 和 [Inno Setup 6](https://jrsoftware.org/isdl.php)。普通用户只需下载生成的安装程序或 ZIP。

```powershell
go test ./...
go vet ./...
powershell -NoProfile -File scripts/package-windows.ps1
powershell -NoProfile -File scripts/test-windows-package.ps1
```

构建输出在忽略目录 `artifacts/releases`，含便携 ZIP、安装 EXE 和 SHA-256 清单。脚本复用当前版本文件，编译时嵌入 revision。Inno Setup 采用 [当前用户安装模式](https://jrsoftware.org/ishelp/topic_setup_privilegesrequired.htm)，卸载不配置数据目录删除规则。

未安装 Go 时，可用 Docker 构建同一 Windows 二进制，再调用打包脚本：

```powershell
$revision = git rev-parse HEAD
docker build --target windows-binary --build-arg "VCS_REF=$revision" --output type=local,dest=artifacts/windows-binary .
powershell -NoProfile -File scripts/package-windows.ps1 -BinaryPath artifacts/windows-binary/dns-assistant.exe
```

`test-windows-package.ps1` 在临时目录检查校验和、解包、安装、开始菜单参数、同版本重装、卸载与数据保留；若用户环境已安装 DNS Assistant 或存在同名开始菜单目录，主动停止以免覆盖。Go 生命周期测试另行验证 DNS、管理、正常停止与重启。

GitHub Actions 在 PR 验证 Windows 构建和生命周期测试；PR／main／标签／手动运行均上传安装程序、ZIP 与校验文件为 Actions artifacts。版本标签在测试、Windows 包和 Docker 双架构验证通过后自动发布 GitHub Release，上传安装程序、ZIP、SHA-256 清单和对应 `docs/releases/vX.Y.Z.md` 说明。工作流使用预装 Inno Setup 的 [Windows 2025 runner](https://github.com/actions/runner-images/blob/main/images/windows/Windows2025-Readme.md)。本地证据和未验证项见仓库的 `docs/validation.md`。

## 下载代理候选

当前本地候选的单机模式随 DNS 与管理服务一起启动 TCP 8082 代理；退出时关闭代理。升级需备份完整数据目录，schema 4 回退旧程序须恢复匹配备份。官方安装包仍按公开 Release 核对，不能将本地候选当成已发布安装包。
