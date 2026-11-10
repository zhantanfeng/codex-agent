package dev.codexremote.app

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ConnectionHealthTest {
    @Test
    fun hoursWithoutModelOutputStayConnectedWhenComputerAnswersHeartbeats() {
        val health = ConnectionHealth()
        health.received(0)
        // Simulate a twelve-hour turn with only the five-second presence replies.
        for (now in 5_000L..43_200_000L step 5_000L) {
            assertFalse(health.isOffline(now))
            assertFalse(health.needsReconnect(now))
            health.received(now)
        }
    }

    @Test
    fun missedHeartbeatsFirstShowOfflineThenRecoverTheSocket() {
        val health = ConnectionHealth()
        health.received(1_000)
        assertFalse(health.isOffline(15_999))
        assertTrue(health.isOffline(16_000))
        assertFalse(health.needsReconnect(45_999))
        assertTrue(health.needsReconnect(46_000))
    }

    @Test
    fun incomingProgressAndRecoveredConnectionsRestartTheLivenessWindow() {
        val health = ConnectionHealth()
        health.received(0)
        health.received(40_000)
        assertFalse(health.isOffline(50_000))
        assertFalse(health.needsReconnect(80_000))
        assertTrue(health.needsReconnect(85_000))

        health.received(90_000)
        assertFalse(health.isOffline(90_000))
        assertFalse(health.needsReconnect(130_000))
    }

    @Test
    fun returningFromLongBackgroundRebuildsEvenAnApparentlyOpenSocket() {
        val health = ConnectionHealth()
        health.received(0)
        health.backgrounded(1_000)
        health.received(299_000) // A late buffered message cannot prove the socket is usable.
        assertTrue(health.needsForegroundReconnect(301_000, hasSocket = true, connected = true))
        // Consuming the foreground transition must not create a reconnect loop.
        health.received(301_000)
        assertFalse(health.needsForegroundReconnect(301_001, hasSocket = true, connected = true))
    }

    @Test
    fun aBriefBackgroundDoesNotInterruptAHealthyConnectionOrAnInitialDial() {
        val health = ConnectionHealth()
        assertFalse(health.needsForegroundReconnect(1_000, hasSocket = true, connected = false))
        health.received(1_000)
        health.backgrounded(2_000)
        assertFalse(health.needsForegroundReconnect(5_000, hasSocket = true, connected = true))
    }

    @Test
    fun foregroundRestoresMissingSocketsAndStaleConnectionsWithoutWaitingFortyFiveSeconds() {
        val health = ConnectionHealth()
        health.received(1_000)
        assertTrue(health.needsForegroundReconnect(2_000, hasSocket = false, connected = false))
        assertTrue(health.needsForegroundReconnect(16_000, hasSocket = true, connected = true))
    }
}
