package dev.codexremote.app

data class UserFacingError(
    val message: String,
    val sessionInUse: Boolean = false,
    val reconnectRequired: Boolean = false,
) {
    companion object {
        fun fromMessage(message: String): UserFacingError {
            if (message.contains("already has an active writer", ignoreCase = true)) {
                return UserFacingError(
                    message = "请先在电脑端或其他客户端退出该会话，再点击“重试”。也可以返回主界面，选择其他会话。",
                    sessionInUse = true,
                )
            }
            if (message.contains("file already closed", ignoreCase = true) ||
                message.contains("host is offline", ignoreCase = true) ||
                message.contains("app-server is not running", ignoreCase = true) ||
                message.contains("Computer response timed out", ignoreCase = true)
            ) {
                return UserFacingError(
                    message = "暂时无法联系电脑端。请确认电脑已开机且未休眠，并启动或重启电脑端程序，然后点击“重新连接”。",
                    reconnectRequired = true,
                )
            }
            if (message.contains("relay is not connected", ignoreCase = true)) return connectionUnavailable()
            return UserFacingError(message)
        }

        fun connectionUnavailable() = UserFacingError(
            message = "手机与服务器的连接已断开。请检查网络，然后点击“重新连接”。",
            reconnectRequired = true,
        )
    }
}
