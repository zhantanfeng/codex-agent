package dev.codexremote.app

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import org.bouncycastle.jce.provider.BouncyCastleProvider
import org.json.JSONObject
import java.nio.charset.StandardCharsets
import java.security.KeyFactory
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.MessageDigest
import java.security.PrivateKey
import java.security.PublicKey
import java.security.SecureRandom
import java.security.Signature
import java.security.spec.PKCS8EncodedKeySpec
import java.security.spec.X509EncodedKeySpec
import java.util.Base64
import java.util.UUID
import java.util.concurrent.atomic.AtomicLong
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

data class SignedEnvelope(
    val version: Int,
    val senderId: String,
    val targetId: String,
    val sessionId: String,
    val sequence: Long,
    val timestamp: Long,
    val nonce: String,
    val kind: String,
    val payload: String,
    val signature: String,
) {
    fun toJson(): JSONObject = JSONObject()
        .put("version", version)
        .put("senderId", senderId)
        .put("targetId", targetId)
        .put("sessionId", sessionId)
        .put("sequence", sequence)
        .put("timestamp", timestamp)
        .put("nonce", nonce)
        .put("kind", kind)
        .put("payload", payload)
        .put("signature", signature)

    fun decodedPayload(): JSONObject = JSONObject(String(b64decode(payload), StandardCharsets.UTF_8))

    companion object {
        fun fromJson(value: JSONObject) = SignedEnvelope(
            version = value.getInt("version"),
            senderId = value.getString("senderId"),
            targetId = value.getString("targetId"),
            sessionId = value.getString("sessionId"),
            sequence = value.getLong("sequence"),
            timestamp = value.getLong("timestamp"),
            nonce = value.getString("nonce"),
            kind = value.getString("kind"),
            payload = value.getString("payload"),
            signature = value.getString("signature"),
        )
    }
}

class IdentityStore(context: Context) {
    private val prefs = context.getSharedPreferences("identity", Context.MODE_PRIVATE)
    private val keyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
    val clientId: String = prefs.getString("client_id", null) ?: "phone-${UUID.randomUUID()}".also {
        prefs.edit().putString("client_id", it).apply()
    }
    val keyPair: KeyPair by lazy { loadOrCreateKeyPair() }

    private fun loadOrCreateKeyPair(): KeyPair {
        val publicValue = prefs.getString("public", null)
        val privateValue = prefs.getString("private", null)
        if (publicValue != null && privateValue != null) {
            runCatching {
                val public = KeyFactory.getInstance("Ed25519", edProvider).generatePublic(X509EncodedKeySpec(b64decode(publicValue)))
                val protected = b64decode(privateValue)
                require(protected.size > 12) { "Invalid protected identity" }
                val cipher = Cipher.getInstance("AES/GCM/NoPadding")
                cipher.init(Cipher.DECRYPT_MODE, encryptionKey(), GCMParameterSpec(128, protected.copyOfRange(0, 12)))
                val privateBytes = cipher.doFinal(protected.copyOfRange(12, protected.size))
                val private = KeyFactory.getInstance("Ed25519", edProvider).generatePrivate(PKCS8EncodedKeySpec(privateBytes))
                return KeyPair(public, private)
            }.onFailure {
                prefs.edit().remove("public").remove("private").apply()
            }
        }
        val pair = KeyPairGenerator.getInstance("Ed25519", edProvider).generateKeyPair()
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, encryptionKey())
        val protected = cipher.iv + cipher.doFinal(pair.private.encoded)
        prefs.edit()
            .putString("public", b64(pair.public.encoded))
            .putString("private", b64(protected))
            .apply()
        return pair
    }

    private fun encryptionKey(): SecretKey {
        (keyStore.getKey("codex_remote_identity", null) as? SecretKey)?.let { return it }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        generator.init(
            KeyGenParameterSpec.Builder(
                "codex_remote_identity",
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            ).setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }
}

class EnvelopeCodec(private val identity: IdentityStore) {
    private val sequence = AtomicLong(0)
    private val sessionId = randomToken(18)
    private val lastSequences = mutableMapOf<String, Long>()

    fun sign(target: String, kind: String, payload: JSONObject): SignedEnvelope {
        val encodedPayload = b64(payload.toString().toByteArray(StandardCharsets.UTF_8))
        val unsigned = SignedEnvelope(
            version = 1,
            senderId = identity.clientId,
            targetId = target,
            sessionId = sessionId,
            sequence = sequence.incrementAndGet(),
            timestamp = System.currentTimeMillis(),
            nonce = randomToken(18),
            kind = kind,
            payload = encodedPayload,
            signature = "",
        )
        val signer = Signature.getInstance("Ed25519", edProvider)
        signer.initSign(identity.keyPair.private)
        signer.update(canonical(unsigned))
        return unsigned.copy(signature = b64(signer.sign()))
    }

    @Synchronized
    fun verify(envelope: SignedEnvelope, publicKey: PublicKey) {
        require(envelope.version == 1) { "Unsupported protocol version" }
        val now = System.currentTimeMillis()
        require(envelope.timestamp in (now - 300_000)..(now + 30_000)) { "Expired message" }
        val verifier = Signature.getInstance("Ed25519", edProvider)
        verifier.initVerify(publicKey)
        verifier.update(canonical(envelope))
        require(verifier.verify(b64decode(envelope.signature))) { "Invalid signature" }
        val replayKey = "${envelope.senderId}:${envelope.sessionId}"
        require(envelope.sequence > (lastSequences[replayKey] ?: 0)) { "Replayed message" }
        lastSequences[replayKey] = envelope.sequence
    }

    private fun canonical(value: SignedEnvelope): ByteArray {
        val hash = MessageDigest.getInstance("SHA-256").digest(value.payload.toByteArray(StandardCharsets.UTF_8))
        val hashHex = hash.joinToString("") { "%02x".format(it) }
        return listOf(
            value.version.toString(), value.senderId, value.targetId, value.sessionId,
            value.sequence.toString(), value.timestamp.toString(), value.nonce, value.kind, hashHex,
        ).joinToString("\n").toByteArray(StandardCharsets.UTF_8)
    }
}

private val edProvider = BouncyCastleProvider()

fun parsePublicKey(value: String): PublicKey = KeyFactory.getInstance("Ed25519", edProvider)
    .generatePublic(X509EncodedKeySpec(b64decode(value)))

fun b64(value: ByteArray): String = Base64.getUrlEncoder().withoutPadding().encodeToString(value)
fun b64decode(value: String): ByteArray = Base64.getUrlDecoder().decode(value)

private fun randomToken(size: Int): String = ByteArray(size).also { SecureRandom().nextBytes(it) }.let(::b64)
