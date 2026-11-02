# Windows 单机安装

支持 Windows 10／11 x64。安装包与便携版均包含管理页面，使用 WebView2（Windows 内嵌网页组件）显示在程序自己的窗口中，无需 Docker、Go、Node.js 或额外数据库。需要 [Microsoft Edge WebView2 Evergreen Runtime](https://developer.microsoft.com/microsoft-edge/webview2/)；缺失或损坏时显示错误，安装或修复后从托盘重试，不会改为打开浏览器。电脑需要固定局域网 IPv4，并在主机使用期间保持运行；退出程序或电脑休眠会停止 DNS。

Windows 发布包按图形应用构建，普通启动、登录后启动和更新重启不创建命令行窗口。仅显式使用 `-tray=false` 时连接已有终端或创建调试控制台；命令行的 `version`、`config` 等输出仍可重定向。

## 安装与启动

从项目 Release 或构建产物中选择 `dns-assistant-X.Y.Z-windows-amd64-setup.exe`。安装到当前用户目录，无需管理员权限；开始菜单的 **DNS Assistant** 或直接双击安装目录里的 `dns-assistant.exe` 启动服务，就绪后显示管理窗口，程序常驻右下角托盘。**Management** 和重复启动 EXE 均唤回已有窗口。关闭窗口只隐藏到托盘，退出使用托盘菜单。安装不修改系统 DNS、不添加防火墙规则，开机启动默认关闭。

便携版使用 `dns-assistant-X.Y.Z-windows-amd64.zip`，解压至可写目录，双击 `dns-assistant.exe` 即可启动并打开管理页。便携版默认把数据保存在程序旁的 `data` 目录；安装版通过 `installed.flag` 标记，无论双击 EXE 或使用开始菜单，均使用 `%LOCALAPPDATA%\DNS Assistant\data`。显式 `-data` 或 `DATA_DIR` 可覆盖默认目录。Windows 同一程序目录只允许一个单机服务或更新任务运行。

只需托盘运行时，使用 `dns-assistant.exe standalone -open-window=false`。旧参数 `-open-browser` 保留兼容，在 Windows 上控制内嵌窗口。管理窗口加载失败不会停止 DNS 和托盘；修复原因后可从托盘重试。局域网管理地址仍可手动访问。页面内的项目链接留在程序窗口，托盘“打开管理窗口”可回到本地管理页面。

## 托盘管理

右键通知区域的 DNS Assistant 图标打开菜单；图标可能位于任务栏的隐藏图标区域。双击图标打开管理页。

- **打开管理窗口**：显示已有窗口并保留本地页面状态；最小化后可还原。
- **检查更新**：检查过程中保持服务运行。发现新版后询问是否更新；确认后等待 DNS、管理和代理停止，备份完整数据，校验并安装更新，再以原数据与监听参数启动。下载或校验失败会恢复启动旧程序；文件恢复不完整时停止自动重启并显示错误。
- **开机启动（登录后）**：菜单勾选表示已启用。只作用于当前 Windows 用户，使用 [Run 注册表项](https://learn.microsoft.com/en-us/windows/win32/setupapi/run-and-runonce-registry-keys)，不需要管理员权限；实际在登录后启动，保持托盘运行而不弹出管理窗口。程序目录的 `.startup.json` 保存数据路径、地址与端口。再次点击关闭；另一个目录已占用启动项时提示先关闭它。移动便携目录前先关闭开机启动，在新目录运行后重新开启。安装版卸载只清除属于该安装目录的启动项。
- **打开数据目录**：查看数据库、DNS 快照及 `desktop.log`。日志达到约 2 MiB 后轮换，保留当前文件和上一份 `.1`。
- **退出**：停止全部监听与后台任务，关闭数据库并移除托盘图标。退出后游戏主机不能继续使用该 DNS 或代理。

需要控制台运行时使用 `dns-assistant.exe standalone -tray=false -open-window=false`，按 Ctrl+C 退出；控制台模式不创建管理窗口。`-web` 与 `-proxy` 分别指定管理和代理监听地址；开机启动与更新重启保留这些参数，并保持静默托盘运行。托盘图标会在 Windows 资源管理器重新创建通知区域后恢复。开机启动不保证电脑未登录、休眠或关机期间提供服务。

WebView2 缓存按数据目录隔离，保存在 `%LOCALAPPDATA%\DNS Assistant\WebView2`，不混入服务数据备份。窗口支持缩放、最大化和键盘操作；配置备份下载和恢复文件选择由内嵌组件处理。

程序自动发现本机局域网 IPv4，首页的 DNS 与代理配置直接显示地址，无需填写 `Server IP`。多个地址时使用已有下拉列表选择与游戏主机处于同一局域网的地址；无可用地址时显示检查提示，不使用公网、环回或链路本地地址。

需要固定显示某个地址时，可在开始菜单快捷方式末尾添加 `-ip 192.168.1.20`（替换为服务主机地址），或使用 `SERVER_IP` 环境变量。显式值优先，无效值不会改用其他网卡。启动示例：

```powershell
.\dns-assistant.exe standalone -data "$env:LOCALAPPDATA\DNS Assistant\data"
```

便携模式不指定 `-data` 时使用便携目录；地址仍需通过 DHCP 保留或静态配置保持稳定。修改启动参数后重新启动程序生效。

首次运行检查 TCP／UDP 53 与 TCP 8080 无冲突。Windows 防火墙中只允许受信任的专用局域网访问本程序的 UDP／TCP 53 和 TCP 8080；不要允许公共网络或公网转发。程序保留局域网来源、Host 与写操作校验；使用其他主机名访问时设置 `ADMIN_HOSTS`。不要直接关闭防火墙解决访问问题。

## 运行与维护

- DNS、下载代理与管理在同一进程内直接通信，无内部 HTTP 端口或服务密钥。Windows 与 Docker 使用同一运行方式，程序退出时共同停止。
- 数据包含 `controller/assistant.db` 及 DNS 快照 `dns/policy.json`。手动升级前从托盘退出，备份完整数据目录；不要在程序写数据库时只复制 `.db`。托盘发起更新会在停止后自动备份数据。数据放在本地磁盘，不使用 SMB／NFS。
- 安装版升级使用同一安装目录和开始菜单数据路径。卸载保留数据；需要清除时，先停止服务、让游戏主机恢复自动 DNS，再手动删除数据目录。
- 安装版和便携版可使用下面的本地更新入口。也可将新便携版解压至新目录，停止旧版后复制完整 `data`，或始终使用 `-data` 指向独立数据目录。回滚同时恢复与旧版兼容的数据备份。
- PowerShell 使用 `.\dns-assistant.exe version | Out-String` 查看版本及源码 revision，管道会等待图形程序输出完成；`config | ConvertFrom-Json` 读取默认配置。DNS 存活检查使用 `$env:DNS_HEALTH_ADDR='127.0.0.1:53'; .\dns-assistant.exe health dns | Out-String`；管理检查使用 `.\dns-assistant.exe health | Out-String`（默认 8080）。
- 排查端口可使用 `Get-NetTCPConnection -State Listen -LocalPort 53,8080` 与 `Get-NetUDPEndpoint -LocalPort 53`。端口冲突会报告地址及错误，不会结束其他程序。仅环回测试时可用 `standalone -listen 127.0.0.1:5353`；主机接入仍需要标准 53 端口。

安装包暂未代码签名；请核对来源和随包发布的 `SHA256SUMS`。构建和隔离测试不代表防火墙、睡眠唤醒或真实主机下载收益已经验证。

## 本地更新

优先从托盘选择“检查更新”，可自动完成停止、数据备份与重启。下面的独立入口用于手动维护和离线更新：

1. 从托盘退出服务，备份完整数据目录；控制台模式按 Ctrl+C。
2. 双击程序旁的 `Update.cmd`；安装版也可使用开始菜单的 **Update**。它通过 [GitHub Release API](https://docs.github.com/en/rest/releases/releases#get-the-latest-release) 检查最新正式版，仅在版本号更高时下载对应 Windows ZIP 与 `SHA256SUMS`，校验后替换程序文件。
3. 看到更新成功后，重新启动 `dns-assistant.exe`。更新不会自动启动服务或改变系统 DNS。

无法联网时，从可信的项目 Release 获取 ZIP 与同次发布的 `SHA256SUMS`。在程序目录打开 PowerShell 执行：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\update.ps1 -Package "C:\Downloads\dns-assistant-X.Y.Z-windows-amd64.zip"
```

校验清单默认位于 ZIP 同目录，也可用 `-Checksums` 指定。离线更新允许同版本重新安装，拒绝降级；旧版没有更新入口时，先用新版安装程序升级，或按前文手动迁移便携目录。

更新只替换 EXE、说明、许可和更新工具，保留 `data`、独立数据目录、开机启动配置及安装标记。服务仍运行、校验失败、包内含数据文件或越界路径时拒绝更新。复制失败会尝试恢复旧程序；恢复失败会给出备份路径。旧程序 ZIP 留在 `.update-backups`；托盘更新另有同名 `-data.zip` 保存停止后的完整数据，手动运行更新脚本默认不备份数据。回滚时先退出程序，再同时恢复匹配的程序与数据 ZIP。确认新版可用后可手动删除旧备份。下载或校验失败后可重新运行更新入口。

## 从源码打包

管理页、窗口、托盘、EXE 和安装程序共用 `web/public/logo.svg`。Windows 多尺寸图标 `deploy/windows/app.ico` 与 `cmd/dns-assistant/resource_windows_amd64.syso` 随源码保存，普通构建无需额外图标工具。修改 Logo 后使用 Sharp 0.35.5 运行 `node scripts/generate-icons.cjs`，脚本通过固定版本的 `rsrc` 重新生成资源；`SHARP_MODULE` 和 `GO_BINARY` 可指定构建工具路径。

维护者需要 Go 1.27.2、Node.js 24 和 [Inno Setup 6](https://jrsoftware.org/isdl.php)。普通用户只需下载生成的安装程序或 ZIP。

```powershell
go test ./...
go vet ./...
powershell -NoProfile -File scripts/package-windows.ps1
powershell -NoProfile -File scripts/test-windows-package.ps1
powershell -NoProfile -File scripts/test-windows-update.ps1
powershell -NoProfile -File scripts/test-windows-tray.ps1
```

构建输出在忽略目录 `artifacts/releases`，含便携 ZIP、安装 EXE 和 SHA-256 清单。脚本复用当前版本文件，编译时嵌入 revision。Inno Setup 采用 [当前用户安装模式](https://jrsoftware.org/ishelp/topic_setup_privilegesrequired.htm)，卸载不配置数据目录删除规则。

未安装 Go 时，可用 Docker 构建同一 Windows 二进制，再调用打包脚本：

```powershell
$revision = git rev-parse HEAD
docker build --target windows-binary --build-arg "VCS_REF=$revision" --output type=local,dest=artifacts/windows-binary .
powershell -NoProfile -File scripts/package-windows.ps1 -BinaryPath artifacts/windows-binary/dns-assistant.exe
```

`test-windows-package.ps1` 在临时目录检查校验和、解包、安装、开始菜单参数、同版本重装、卸载与数据保留；使用同一安装脚本和程序文件，以独立测试安装标识及开始菜单分组编译安装器，保留已有安装。`test-windows-update.ps1` 验证离线更新、校验失败、非法压缩包、运行锁、数据保留及复制中途失败恢复。Go 生命周期测试另行验证 DNS、管理、正常停止与重启。

`TestInstalledTrayActions` 用于已安装程序的交互验收，默认跳过。显式设置 `DNS_ASSISTANT_INSTALLED_BINARY` 为 EXE 绝对路径、`DNS_ASSISTANT_INSTALLED_PID` 为对应进程 ID 后，运行 `go test ./cmd/dns-assistant -run '^TestInstalledTrayActions$' -count=1 -v`。测试会调用实际托盘菜单命令，联网检查更新但不安装远程版本，切换开机启动，退出并按登录启动参数重启；期间服务会短暂中断。结束时恢复启动设置并重新打开管理窗口。它验证登录启动命令，不代替真实注销或重启后的验收，不应加入无人值守 CI。

GitHub Actions 在 PR 验证 Windows 构建和生命周期测试；PR／main／标签／手动运行均上传安装程序、ZIP 与校验文件为 Actions artifacts。版本标签在测试、Windows 包和 Docker 双架构验证通过后自动发布 GitHub Release，上传安装程序、ZIP、SHA-256 清单和对应 `docs/releases/vX.Y.Z.md` 说明。工作流使用预装 Inno Setup 的 [Windows 2025 runner](https://github.com/actions/runner-images/blob/main/images/windows/Windows2025-Readme.md)。本地证据和未验证项见仓库的 `docs/validation.md`。

`test-windows-tray.ps1` 需要已登录且有 Windows 资源管理器的桌面，使用隔离数据及本地高端口验证真实 EXE 的托盘注册、图标恢复、菜单退出、更新后重启和失败后恢复启动。常规 Go 测试在隔离注册表项中验证开机启动开关与参数保存，不修改真实登录启动项；实际注销／重登仍需人工验收。

## 下载代理候选

当前本地候选的单机模式随 DNS 与管理服务一起启动 TCP 8082 代理；退出时关闭代理。升级需备份完整数据目录，schema 4 回退旧程序须恢复匹配备份。官方安装包仍按公开 Release 核对，不能将本地候选当成已发布安装包。
