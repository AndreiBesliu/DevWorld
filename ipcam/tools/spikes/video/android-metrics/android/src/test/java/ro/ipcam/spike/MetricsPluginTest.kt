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
    fun onCharger_pluggedInCountsAlsoWhenTheBatteryDoesNotCharge() {
        // EXTRA_PLUGGED: 0 on battery, BATTERY_PLUGGED_AC (1), USB (2) or WIRELESS (4) on a charger.
        assertEquals(false, onCharger(0, false))
        assertEquals(true, onCharger(1, true))
        // Full, or held at 85 % by the battery protection: plugged in but not charging (R48).
        assertEquals(true, onCharger(2, false))
        assertEquals(true, onCharger(4, null))
        // Without the sticky intent, isCharging decides; without either, the shell cannot tell.
        assertEquals(true, onCharger(null, true))
        assertEquals(false, onCharger(null, false))
        assertNull(onCharger(null, null))
    }

    @Test
    fun autorunQuery_launchExtrasBecomeTheAppsQueryString() {
        // R5: adb shell am start ... --es autorun S1 --es hub http://10.0.2.2:7080
        assertEquals("autorun=S1&hub=http%3A%2F%2F10.0.2.2%3A7080", autorunQuery("S1", "http://10.0.2.2:7080"))
        assertEquals("autorun=S1", autorunQuery("S1", null))
        assertNull(autorunQuery(null, "http://10.0.2.2:7080"))
    }

    @Test
    fun autorunHandoff_aRecreatedActivityRunsTheScenarioItsPageNeverGot() {
        // The emulator job's first cold start (3b35760): the system created the Activity again
        // (changingConfigurations=true) before the first one had handed the scenario to its
        // page. load() had taken the extras off the Intent, so the second Activity saw none.
        val first = AutorunHandoff.onLoad(extrasQuery = "autorun=S1", restored = false, savedPending = null)
        assertEquals("autorun=S1", first.pendingQuery)
        val saved = first.pendingQuery // onSaveInstanceState before the page got the query
        val second = AutorunHandoff.onLoad(extrasQuery = null, restored = true, savedPending = saved)
        assertEquals("autorun=S1", second.pendingQuery)
        second.loaded()
        second.handed()
        assertNull(second.pendingQuery)
    }

    @Test
    fun autorunHandoff_aRestoredActivityDoesNotRunAScenarioItsPageHadAgain() {
        // Process death: the page ran S1, the app went to the background, the system killed its
        // process and restored the Activity from the launch Intent it kept, extras included.
        val first = AutorunHandoff.onLoad(extrasQuery = "autorun=S1", restored = false, savedPending = null)
        first.loaded()
        first.handed()
        val restored = AutorunHandoff.onLoad(extrasQuery = "autorun=S1", restored = true, savedPending = first.pendingQuery)
        assertNull(restored.pendingQuery)
        // A launch without extras runs nothing.
        assertNull(AutorunHandoff.onLoad(extrasQuery = null, restored = false, savedPending = null).pendingQuery)
    }

    @Test
    fun autorunHandoff_aRecreationBeforeThePageReadRunsTheScenarioAgain() {
        // Final fix C review, M7: the plugin loaded the page with the scenario's query, and the
        // system created the Activity again before that page had read it. A page reads the
        // metrics first thing when its run starts; before that read it has not taken the
        // scenario, so the new Activity must run it.
        val first = AutorunHandoff.onLoad(extrasQuery = "autorun=S1", restored = false, savedPending = null)
        first.loaded() // loadUrl
        val saved = first.pendingQuery // onSaveInstanceState before the page's read
        val second = AutorunHandoff.onLoad(extrasQuery = null, restored = true, savedPending = saved)
        assertEquals("autorun=S1", second.pendingQuery)
        second.loaded()
        second.handed() // the new page's run read the metrics: it has the scenario
        assertNull(second.pendingQuery)
    }

    @Test
    fun autorunHandoff_aReadBeforeTheQueryWasLoadedIsAnotherPages() {
        // A read before loadUrl comes from the bridge's first page, which has no scenario.
        val handoff = AutorunHandoff.onLoad(extrasQuery = "autorun=S1", restored = false, savedPending = null)
        handoff.handed()
        assertEquals("autorun=S1", handoff.pendingQuery)
        handoff.loaded()
        assertEquals("autorun=S1", handoff.pendingQuery)
        handoff.handed()
        assertNull(handoff.pendingQuery)
    }

    @Test
    fun pageForLog_dropsTheHubAddressAndKeepsTheScenario() {
        assertEquals(
            "http://localhost/ (autorun S1)",
            pageForLog("http://localhost/?autorun=S1&hub=http%3A%2F%2F10.0.2.2%3A7080"),
        )
        assertEquals("http://localhost/", pageForLog("http://localhost/"))
        assertEquals("http://localhost/ (autorun S1)", pageForLog("http://localhost/?hub=http%3A%2F%2Fx&autorun=S1#top"))
        assertEquals("none", pageForLog(null))
    }
}
