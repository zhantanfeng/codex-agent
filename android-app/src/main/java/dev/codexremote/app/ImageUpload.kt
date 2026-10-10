package dev.codexremote.app

import org.json.JSONObject
import org.json.JSONArray
import java.io.File
import java.security.MessageDigest
import java.util.Base64

const val MAX_MESSAGE_IMAGES = 4
const val MAX_IMAGE_BYTES = 8 * 1024 * 1024
const val IMAGE_CHUNK_BYTES = 192 * 1024

data class DraftImage(
    val id: String,
    val name: String,
    val path: String,
    val previewPath: String,
    val mimeType: String,
)

data class MessageImage(val id: String, val name: String, val previewPath: String? = null)

internal fun parseUserMessage(item: JSONObject): MessageUi {
    val content = item.optJSONArray("content") ?: JSONArray()
    val values = (0 until content.length()).mapNotNull { content.optJSONObject(it) }
    val text = values.filter { it.optString("type") == "text" }.joinToString("\n") { it.optString("text") }
    val images = values.filter { it.optString("type") == "localImage" || it.optString("type") == "image" }
        .map { MessageImage(it.optString("attachmentId"), it.optString("name", "图片")) }
    return MessageUi(item.optString("id"), "user", text, images = images)
}

internal suspend fun uploadImage(
    threadId: String,
    image: DraftImage,
    request: suspend (String, JSONObject) -> JSONObject,
    progress: (Int) -> Unit,
): String {
    val bytes = File(image.path).readBytes()
    require(bytes.isNotEmpty() && bytes.size <= MAX_IMAGE_BYTES) { "请选择不超过 8 MB 的图片" }
    val digest = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
    val identity = JSONObject().put("threadId", threadId).put("id", image.id)
    val begin = request(
        "image.begin",
        JSONObject(identity.toString()).put("name", image.name).put("mimeType", image.mimeType)
            .put("size", bytes.size).put("sha256", digest),
    )
    var offset = begin.getInt("offset")
    require(offset in 0..bytes.size) { "图片上传进度无效，请重新选择图片" }
    progress(offset * 100 / bytes.size)
    if (!begin.optBoolean("complete")) {
        while (offset < bytes.size) {
            val end = (offset + IMAGE_CHUNK_BYTES).coerceAtMost(bytes.size)
            val chunk = Base64.getUrlEncoder().withoutPadding().encodeToString(bytes.copyOfRange(offset, end))
            val response = request("image.chunk", JSONObject(identity.toString()).put("offset", offset).put("bytes", chunk))
            require(response.getInt("offset") == end) { "图片上传进度不匹配，请重试上传" }
            offset = end
            progress(offset * 100 / bytes.size)
        }
        request("image.finish", identity)
    }
    progress(100)
    return image.id
}
