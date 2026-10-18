package ro.ipcam.spike

import android.os.PowerManager
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class MetricsPluginTest {
    @Test
    fun thermalStatusMapsAllLevels() {
        val statuses = listOf(
            PowerManager.THERMAL_STATUS_NONE,
            PowerManager.THERMAL_STATUS_LIGHT,
            PowerManager.THERMAL_STATUS_MODERATE,
            PowerManager.THERMAL_STATUS_SEVERE,
            PowerManager.THERMAL_STATUS_CRITICAL,
            PowerManager.THERMAL_STATUS_EMERGENCY,
            PowerManager.THERMAL_STATUS_SHUTDOWN,
        )

        // The names and their order are THERMAL_LEVELS of the spike app (src/native.ts).
        assertEquals(
            listOf("NONE", "LIGHT", "MODERATE", "SEVERE", "CRITICAL", "EMERGENCY", "SHUTDOWN"),
            statuses.map(::thermalName),
        )
        assertNull(thermalName(statuses.max() + 1))
    }

    @Test
    fun autorunQuery_launchExtrasBecomeTheAppsQueryString() {
        // R5: adb shell am start ... --es autorun S1 --es hub http://10.0.2.2:7080
        assertEquals("autorun=S1&hub=http%3A%2F%2F10.0.2.2%3A7080", autorunQuery("S1", "http://10.0.2.2:7080"))
        assertEquals("autorun=S1", autorunQuery("S1", null))
        assertNull(autorunQuery(null, "http://10.0.2.2:7080"))
    }
}
