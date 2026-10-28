package app

// Windows 不提供 IANA 时区库；单机包不能依赖预装 Go。
import _ "time/tzdata"
