package dev.codexremote.app

import android.content.Context
import android.net.Uri
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONObject
import java.net.URI
import java.net.URLDecoder
import java.nio.charset.StandardCharsets
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit
import java.util.Timer
import kotlin.concurrent.fixedRateTimer
import kotlin.concurrent.schedule

data class PairingConfig(
    val relayUrl: String,
    val hostId: String,
    val hostPublicKey: String,
    val code: String,
)

data class RemoteCommand(
    val action: String,
    val dataJson: String,
) {
    val retryable: Boolean
        get() = action == "thread.start" || action == "thread.resume"

    fun data(): JSONObject = JSONObject(dataJson)

    companion object {
        fun capture(action: String, data: JSONObject) = RemoteCommand(action, data.toString())
    }
}

class RemoteClient(
    context: Context,
    private val listener: Listener,
) {
    interface Listener {
        fun onConnection(status: ConnectionStatus, detail: String = "")
        fun onPaired()
        fun onResponse(command: RemoteCommand, payload: JSONObject)
        fun onEvent(kind: String, payload: JSONObject)
    }

    enum class ConnectionStatus { DISCONNECTED, CONNECTING, CONNECTED, ERROR }

    private val prefs = context.getSharedPreferences("connection", Context.MODE_PRIVATE)
    private val identity = IdentityStore(context)
    private val codec = EnvelopeCodec(identity)
    private val http = OkHttpClient.Builder()
        .pingInterval(25, TimeUnit.SECONDS)
        .retryOnConnectionFailure(true)
        .build()
    private val pending = ConcurrentHashMap<String, RemoteCommand>()
    private var socket: WebSocket? = null
    private var pairingTimer: Timer? = null
    private var reconnectTimer: Timer? = null
    private var reconnectDelayMs = 1_000L
    @Volatile private var connectionGeneration = 0
    private var shouldReconnect = false
    private var config: PairingConfig? = loadConfig()
    var paired: Boolean = prefs.getBoolean("paired", false)
        private set

    fun configured(): Boolean = config != null

    fun configure(raw: String) {
        val parsed = parsePairing(raw)
        config = parsed
        paired = false
        prefs.edit()
            .putString("relay_url", parsed.relayUrl)
            .putString("host_id", parsed.hostId)
            .putString("host_public", parsed.hostPublicKey)
            .putString("pair_code", parsed.code)
            .putBoolean("paired", false)
            .apply()
    }

    @Synchronized
    fun connect() {
        val current = config ?: return listener.onConnection(ConnectionStatus.ERROR, "No pairing configuration")
        shouldReconnect = true
        reconnectTimer?.cancel()
        socket?.cancel()
        connectionGeneration += 1
        val generation = connectionGeneration
        listener.onConnection(ConnectionStatus.CONNECTING)
        val separator = if (current.relayUrl.contains('?')) '&' else '?'
        val endpoint = "${current.relayUrl}${separator}role=client&host_id=${Uri.encode(current.hostId)}&client_id=${Uri.encode(identity.clientId)}"
        socket = http.newWebSocket(Request.Builder().url(endpoint).build(), object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                if (generation != connectionGeneration) return
                reconnectDelayMs = 1_000L
                sendEnvelope(
                    "hello",
                    JSONObject().put("publicKey", b64(identity.keyPair.public.encoded)),
                )
                if (!paired) {
                    sendEnvelope(
                        "pair.request",
                        JSONObject()
                            .put("code", current.code)
                            .put("clientId", identity.clientId)
                            .put("publicKey", b64(identity.keyPair.public.encoded)),
                    )
                }
                listener.onConnection(ConnectionStatus.CONNECTED)
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                if (generation != connectionGeneration) return
                runCatching {
                    val envelope = SignedEnvelope.fromJson(JSONObject(text))
                    require(envelope.senderId == current.hostId)
                    require(envelope.targetId == identity.clientId || envelope.targetId == "*")
                    codec.verify(envelope, parsePublicKey(current.hostPublicKey))
                    val payload = envelope.decodedPayload()
                    when (envelope.kind) {
                        "pair.accepted" -> {
                            pairingTimer?.cancel()
                            paired = true
                            prefs.edit().putBoolean("paired", true).remove("pair_code").apply()
                            listener.onPaired()
                        }
                        "pair.pending" -> {
                            listener.onEvent(envelope.kind, payload)
                            pairingTimer?.cancel()
                            pairingTimer = fixedRateTimer("pair-retry", daemon = true, initialDelay = 2_000, period = 2_000) {
                                sendEnvelope(
                                    "pair.request",
                                    JSONObject()
                                        .put("code", current.code)
                                        .put("clientId", identity.clientId)
                                        .put("publicKey", b64(identity.keyPair.public.encoded)),
                                )
                            }
                        }
                        "response" -> {
                            val requestId = payload.getString("requestId")
                            listener.onResponse(pending.remove(requestId) ?: RemoteCommand("unknown", "{}"), payload)
                        }
                        else -> listener.onEvent(envelope.kind, payload)
                    }
                }.onFailure { listener.onConnection(ConnectionStatus.ERROR, it.message ?: "Invalid server message") }
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                if (generation != connectionGeneration) return
                listener.onConnection(ConnectionStatus.ERROR, t.message ?: "Connection failed")
                scheduleReconnect(generation)
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                if (generation != connectionGeneration) return
                listener.onConnection(ConnectionStatus.DISCONNECTED, reason)
                if (code != 1000) scheduleReconnect(generation)
            }
        })
    }

    @Synchronized
    fun disconnect() {
        shouldReconnect = false
        connectionGeneration += 1
        reconnectTimer?.cancel()
        socket?.close(1000, "client closed")
        pairingTimer?.cancel()
        socket = null
    }

    @Synchronized
    private fun scheduleReconnect(generation: Int) {
        if (!shouldReconnect || generation != connectionGeneration) return
        reconnectTimer?.cancel()
        val delay = reconnectDelayMs
        reconnectDelayMs = (reconnectDelayMs * 2).coerceAtMost(30_000L)
        reconnectTimer = Timer("relay-reconnect", true).apply {
            schedule(delay) { connect() }
        }
    }

    fun command(action: String, data: JSONObject = JSONObject()): String {
        val requestId = UUID.randomUUID().toString()
        pending[requestId] = RemoteCommand.capture(action, data)
        sendEnvelope(
            "command",
            JSONObject().put("requestId", requestId).put("action", action).put("data", data),
        )
        return requestId
    }

    private fun sendEnvelope(kind: String, payload: JSONObject) {
        val current = config ?: return
        socket?.send(codec.sign(current.hostId, kind, payload).toJson().toString())
    }

    private fun loadConfig(): PairingConfig? {
        val relay = prefs.getString("relay_url", null) ?: return null
        return PairingConfig(
            relayUrl = relay,
            hostId = prefs.getString("host_id", null) ?: return null,
            hostPublicKey = prefs.getString("host_public", null) ?: return null,
            code = prefs.getString("pair_code", "") ?: "",
        )
    }

    companion object {
        fun parsePairing(raw: String): PairingConfig {
            val value = raw.trim()
            val encoded = if (value.startsWith("codexremote://")) {
                URI(value).rawQuery
                    ?.split('&')
                    ?.firstOrNull { it.substringBefore('=') == "data" }
                    ?.substringAfter('=', "")
                    ?.let { URLDecoder.decode(it, StandardCharsets.UTF_8.name()) }
                    ?.takeIf { it.isNotEmpty() }
                    ?: error("Missing pairing data")
            } else value
            val json = JSONObject(String(b64decode(encoded), StandardCharsets.UTF_8))
            require(json.getInt("version") == 1) { "Unsupported pairing version" }
            return PairingConfig(
                relayUrl = json.getString("relayUrl"),
                hostId = json.getString("hostId"),
                hostPublicKey = json.getString("hostPublicKey"),
                code = json.getString("code"),
            )
        }
    }
}
