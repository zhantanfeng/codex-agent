package dev.codexremote.app

import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.io.IOException
import java.net.URI

internal fun isLinkCode(code: String): Boolean = code.length == 6 && code.all { it in '0'..'9' }

internal fun pairingLookupUrl(relayUrl: String): String {
    val relay = URI(relayUrl.trim())
    val scheme = when (relay.scheme) {
        "ws" -> "http"
        "wss" -> "https"
        else -> throw IllegalArgumentException("服务器地址应以 ws:// 或 wss:// 开头")
    }
    require(!relay.host.isNullOrBlank() && relay.userInfo == null) { "服务器地址无效" }
    return URI(scheme, null, relay.host, relay.port, relay.path.removeSuffix("/ws") + "/pairing/resolve", null, null).toString()
}

internal fun resolveLinkCode(http: OkHttpClient, relayUrl: String, code: String): PairingConfig {
    require(isLinkCode(code)) { "请输入六位数字连接码" }
    val request = Request.Builder().url(pairingLookupUrl(relayUrl))
        .post(JSONObject().put("code", code).toString().toRequestBody("application/json".toMediaType())).build()
    http.newCall(request).execute().use { response ->
        val body = response.body?.string().orEmpty()
        val json = runCatching { JSONObject(body) }.getOrNull()
        if (!response.isSuccessful) {
            throw IOException(json?.optString("error")?.takeIf { it.isNotBlank() } ?: when (response.code) {
                404 -> "服务器尚未支持连接码，请先更新电脑端和中转服务"
                429 -> "尝试次数过多，请一分钟后再试"
                else -> "暂时无法连接服务器，请稍后重试"
            })
        }
        require(json != null && json.optInt("version") == 1) { "服务器返回的配对信息无效" }
        val publicKey = json.getString("hostPublicKey")
        parsePublicKey(publicKey)
        return PairingConfig(relayUrl.trim(), json.getString("hostId"), publicKey, json.getString("code"), direct = true)
    }
}
