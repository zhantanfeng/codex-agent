package dev.codexremote.app

enum class RemoteAvailability(val label: String) {
    CHECKING("正在检测远程状态"),
    ENABLED("远程已开启"),
    DISABLED("远程未开启"),
}
