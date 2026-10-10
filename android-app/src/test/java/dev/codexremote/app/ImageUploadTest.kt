package dev.codexremote.app

import kotlinx.coroutines.runBlocking
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.io.IOException
import java.util.Base64

class ImageUploadTest {
    @Test
    fun resumesInterruptedImageAndFinishesOnlyAfterAllChunks() = runBlocking {
        val file = File.createTempFile("image-upload", ".png")
        try {
            val bytes = ByteArray(IMAGE_CHUNK_BYTES * 2 + 23) { (it % 251).toByte() }
            file.writeBytes(bytes)
            val image = DraftImage("image-1", "phone.png", file.path, file.path, "image/png")
            var received = 0
            var interrupted = false
            val actions = mutableListOf<String>()
            val request: suspend (String, JSONObject) -> JSONObject = { action, data ->
                actions += action
                assertEquals("thread-1", data.getString("threadId"))
                when (action) {
                    "image.begin" -> JSONObject().put("offset", received).put("complete", false)
                    "image.chunk" -> {
                        if (received > 0 && !interrupted) {
                            interrupted = true
                            throw IOException("connection lost")
                        }
                        assertEquals(received, data.getInt("offset"))
                        val chunk = Base64.getUrlDecoder().decode(data.getString("bytes"))
                        assertTrue(chunk.contentEquals(bytes.copyOfRange(received, received + chunk.size)))
                        received += chunk.size
                        JSONObject().put("offset", received)
                    }
                    "image.finish" -> {
                        assertEquals(bytes.size, received)
                        JSONObject().put("id", image.id)
                    }
                    else -> error(action)
                }
            }
            try {
                uploadImage("thread-1", image, request) {}
                error("first upload should fail")
            } catch (_: IOException) {
                assertEquals(IMAGE_CHUNK_BYTES, received)
            }
            val progress = mutableListOf<Int>()
            assertEquals("image-1", uploadImage("thread-1", image, request) { progress += it })
            assertEquals(bytes.size, received)
            assertEquals(1, actions.count { it == "image.finish" })
            assertEquals(100, progress.last())
        } finally {
            file.delete()
        }
    }

    @Test
    fun completedUploadIsReusedWithoutSendingBinaryDataAgain() = runBlocking {
        val file = File.createTempFile("image-upload", ".jpg")
        try {
            file.writeBytes(byteArrayOf(1, 2, 3))
            val image = DraftImage("image-1", "phone.jpg", file.path, file.path, "image/jpeg")
            var requests = 0
            val id = uploadImage("thread-1", image, { action, _ ->
                assertEquals("image.begin", action)
                requests++
                JSONObject().put("offset", 3).put("complete", true)
            }) {}
            assertEquals("image-1", id)
            assertEquals(1, requests)
        } finally {
            file.delete()
        }
    }
}
