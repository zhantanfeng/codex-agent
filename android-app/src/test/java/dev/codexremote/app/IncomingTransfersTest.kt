package dev.codexremote.app

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.security.MessageDigest
import java.util.Base64

class IncomingTransfersTest {
    private fun chunks(payload: JSONObject): List<JSONObject> {
        val bytes = payload.toString().toByteArray(Charsets.UTF_8)
        val digest = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
        val total = (bytes.size + IMAGE_CHUNK_BYTES - 1) / IMAGE_CHUNK_BYTES
        return (0 until total).map { index ->
            JSONObject().put("id", "transfer-1").put("kind", "response").put("requestId", "resume-1")
                .put("index", index).put("total", total).put("size", bytes.size).put("sha256", digest)
                .put("bytes", Base64.getUrlEncoder().withoutPadding().encodeToString(bytes.copyOfRange(index * IMAGE_CHUNK_BYTES, minOf((index + 1) * IMAGE_CHUNK_BYTES, bytes.size))))
        }
    }

    @Test
    fun reassemblesLargeHistoryIncludingMultibyteChineseText() {
        val history = "会话历史".repeat(350_000)
        val payload = JSONObject().put("requestId", "resume-1").put("ok", true)
            .put("data", JSONObject().put("history", history))
        val assembler = IncomingTransfers()
        val parts = chunks(payload)

        parts.dropLast(1).forEach { assertNull(assembler.accept(it)) }
        val result = assembler.accept(parts.last())!!

        assertEquals("response", result.kind)
        assertEquals(history, result.payload.getJSONObject("data").getString("history"))
        assertTrue(result.payload.getBoolean("ok"))
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsHistoryWithMissingChunk() {
        val assembler = IncomingTransfers()
        val parts = chunks(JSONObject().put("text", "x".repeat(600_000)))
        assembler.accept(parts[0])
        assembler.accept(parts[2])
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsModifiedPayload() {
        val assembler = IncomingTransfers()
        val part = chunks(JSONObject().put("text", "hello")).single()
        part.put("sha256", "0".repeat(64))
        assembler.accept(part)
    }

    @Test
    fun clearDiscardsInterruptedTransferBeforeReconnect() {
        val assembler = IncomingTransfers()
        val parts = chunks(JSONObject().put("text", "x".repeat(600_000)))
        assembler.accept(parts.first())
        assembler.clear()
        var result: TransferResult? = null
        parts.forEach { result = assembler.accept(it) }
        assertEquals(600_000, result!!.payload.getString("text").length)
    }
}
