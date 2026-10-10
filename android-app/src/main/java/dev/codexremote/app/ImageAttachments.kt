package dev.codexremote.app

import android.content.Context
import android.graphics.Bitmap
import android.graphics.ImageDecoder
import android.net.Uri
import android.provider.OpenableColumns
import java.io.ByteArrayOutputStream
import java.io.File
import java.util.UUID

internal fun prepareImage(context: Context, uri: Uri): DraftImage {
    val resolver = context.contentResolver
    val mime = resolver.getType(uri)
    require(mime == "image/jpeg" || mime == "image/png") { "第一版支持 JPEG 和 PNG 图片" }
    val name = resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor ->
        if (cursor.moveToFirst()) cursor.getString(0) else null
    } ?: "图片"
    val bitmap = ImageDecoder.decodeBitmap(ImageDecoder.createSource(resolver, uri)) { decoder, info, _ ->
        decoder.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
        val width = info.size.width
        val height = info.size.height
        val scale = (2560.0 / maxOf(width, height)).coerceAtMost(1.0)
        decoder.setTargetSize((width * scale).toInt().coerceAtLeast(1), (height * scale).toInt().coerceAtLeast(1))
    }
    try {
        val encoded = ByteArrayOutputStream().use { output ->
            val format = if (mime == "image/png") Bitmap.CompressFormat.PNG else Bitmap.CompressFormat.JPEG
            check(bitmap.compress(format, 90, output)) { "图片处理失败，请重新选择" }
            output.toByteArray()
        }
        require(encoded.size <= MAX_IMAGE_BYTES) { "图片处理后仍超过 8 MB，请选择较小的图片" }
        val id = UUID.randomUUID().toString()
        val directory = File(context.filesDir, "images").apply { mkdirs() }
        val file = File(directory, "$id.${if (mime == "image/png") "png" else "jpg"}")
        file.writeBytes(encoded)
        val scale = (512.0 / maxOf(bitmap.width, bitmap.height)).coerceAtMost(1.0)
        val thumbnail = Bitmap.createScaledBitmap(bitmap, (bitmap.width * scale).toInt().coerceAtLeast(1), (bitmap.height * scale).toInt().coerceAtLeast(1), true)
        val preview = File(directory, "$id.thumb.jpg")
        try {
            preview.outputStream().use { thumbnail.compress(Bitmap.CompressFormat.JPEG, 80, it) }
        } finally {
            if (thumbnail !== bitmap) thumbnail.recycle()
        }
        return DraftImage(id, name, file.absolutePath, preview.absolutePath, mime)
    } finally {
        bitmap.recycle()
    }
}
