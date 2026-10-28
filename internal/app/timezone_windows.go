package app

// Windows 不提供 IANA 时区库；单机包不能依赖本机 Go 安装。
import _ "time/tzdata"
