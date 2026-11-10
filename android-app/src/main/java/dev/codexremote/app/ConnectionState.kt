package dev.codexremote.app

internal fun applyConnectionStatus(state: UiState, status: RemoteClient.ConnectionStatus, detail: String): UiState {
    val disconnected = status == RemoteClient.ConnectionStatus.ERROR || status == RemoteClient.ConnectionStatus.DISCONNECTED
    val connectionError = state.error?.transportUnavailable == true
    return state.copy(
        connection = status,
        remoteAvailability = RemoteAvailability.CHECKING,
        connectionDetail = detail,
        pairingBusy = if (disconnected) false else state.pairingBusy,
        error = when {
            disconnected -> state.error?.takeUnless { it.transportUnavailable } ?: UserFacingError.connectionUnavailable()
            connectionError -> null
            else -> state.error
        },
        closingSession = if (disconnected) false else state.closingSession,
        threadLoaded = false,
        loadingThread = false,
    )
}

internal fun applyRemotePresence(state: UiState, online: Boolean): UiState {
    if (state.connection != RemoteClient.ConnectionStatus.CONNECTED) return state
    return state.copy(
        remoteAvailability = if (online) RemoteAvailability.ENABLED else RemoteAvailability.DISABLED,
        error = if (online && state.error?.transportUnavailable == true) null else state.error,
    )
}
