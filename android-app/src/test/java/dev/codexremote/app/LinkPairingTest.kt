package dev.codexremote.app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LinkPairingTest {
    @Test fun acceptsSixAsciiDigitsIncludingLeadingZeros() {
        assertTrue(isLinkCode("000123"))
        assertTrue(isLinkCode("000000"))
        assertTrue(isLinkCode("999999"))
        assertFalse(isLinkCode("12345"))
        assertFalse(isLinkCode("1234567"))
        assertFalse(isLinkCode("１２３４５６"))
        assertFalse(isLinkCode("12a456"))
    }

    @Test fun derivesLookupAddressFromRelayWithoutConnectionQuery() {
        assertEquals("http://example.com:8080/pairing/resolve", pairingLookupUrl("ws://example.com:8080/ws?role=client"))
        assertEquals("https://example.com/remote/pairing/resolve", pairingLookupUrl("wss://example.com/remote/ws"))
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsInvalidRelayAddress() { pairingLookupUrl("https://example.com/ws") }
}
