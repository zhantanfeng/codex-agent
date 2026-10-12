package dev.codexremote.app

import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.security.MessageDigest
import java.util.Base64

internal data class TransferResult(val kind: String, val payload: JSONObject)

internal class IncomingTransfers {
    private data class Transfer(
        val kind: String, val requestId: String, val size: Int, val total: Int, val digest: String,
        val bytes: ByteArrayOutputStream = ByteArrayOutputStream(), var next: Int = 0,
    )
    private val transfers = mutableMapOf<String, Transfer>()

    @Synchronized
    fun accept(chunk: JSONObject): TransferResult? {
        val id = chunk.getString("id")
        val kind = chunk.getString("kind")
        val requestId = chunk.optString("requestId")
        val size = chunk.getInt("size")
        val total = chunk.getInt("total")
        val index = chunk.getInt("index")
        val digest = chunk.getString("sha256")
        require(size in 1..(32 * 1024 * 1024) && total == (size + IMAGE_CHUNK_BYTES - 1) / IMAGE_CHUNK_BYTES) { "会话数据大小无效" }
        require(index in 0 until total && kind != "transfer.chunk") { "会话数据块无效" }
        val transfer = transfers[id] ?: run {
            require(index == 0 && transfers.size < 4) { "会话数据传输中断，请重新连接" }
            Transfer(kind, requestId, size, total, digest).also { transfers[id] = it }
        }
        require(transfer.kind == kind && transfer.requestId == requestId && transfer.size == size && transfer.total == total && transfer.digest == digest && transfer.next == index) { "会话数据传输顺序无效" }
        val bytes = Base64.getUrlDecoder().decode(chunk.getString("bytes"))
        val expectedSize = minOf(IMAGE_CHUNK_BYTES, size - index * IMAGE_CHUNK_BYTES)
        require(bytes.size == expectedSize) { "会话数据不完整，请重试" }
        transfer.bytes.write(bytes)
        transfer.next++
        if (transfer.next < total) return null
        transfers.remove(id)
        val raw = transfer.bytes.toByteArray()
        val actualDigest = MessageDigest.getInstance("SHA-256").digest(raw).joinToString("") { "%02x".format(it) }
        require(actualDigest == digest) { "会话数据校验失败，请重试" }
        return TransferResult(kind, JSONObject(String(raw, Charsets.UTF_8)))
    }

    @Synchronized
    fun clear() = transfers.clear()
}
