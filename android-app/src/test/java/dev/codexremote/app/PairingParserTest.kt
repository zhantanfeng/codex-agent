package dev.codexremote.app

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Test

class PairingParserTest {
    @Test
    fun parsesPairingPayload() {
        val raw = JSONObject()
            .put("version", 1)
            .put("relayUrl", "ws://203.0.113.10:8080/ws")
            .put("hostId", "host-test")
            .put("hostPublicKey", "public-key")
            .put("code", "PAIR123")
            .toString()
            .toByteArray()
        val parsed = RemoteClient.parsePairing("codexremote://pair?data=${b64(raw)}")
        assertEquals("host-test", parsed.hostId)
        assertEquals("PAIR123", parsed.code)
    }
}

