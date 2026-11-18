// SPIKE-ONLY (plan 2026-10-18-phase0b-video-spike, Task 10): what the video
// spike reads from Android and production does not need. Never shipped.
package ro.ipcam.spike

/**
 * The launch extras' scenario (R5) between the Activity's creation and the
 * page that runs it. The Activity's saved state keeps [pendingQuery], so a
 * scenario survives the Activity being created again before its page took it:
 * the system can do that right after a launch, for instance when it changes
 * the app's resources (overlays) after the device booted, and no
 * `configChanges` entry stops it. The plugin loads the page with the query
 * ([loaded]); the page has taken the scenario when its run reads the metrics,
 * the first thing a scenario does, and says so (`read({ run: true })`, [read]).
 * The page's other reads do not take it: the start page reads the metrics as
 * soon as it loads, before its run does (C1-R30). From then on a restored
 * Activity runs nothing: the scenario ran, or was cut, with the old page, and
 * the next `am start` runs it again. The plugin's calls come from the UI thread
 * and from the plugin's own thread.
 */
internal class AutorunHandoff private constructor(private var query: String?) {
    /** The page was loaded with the query: its run's read is its scenario's. */
    private var loaded = false

    /** The query string that the page has not taken yet, or null. */
    val pendingQuery: String?
        @Synchronized get() = query

    /** The plugin loaded the page with the query (loadUrl). */
    @Synchronized
    fun loaded() {
        loaded = true
    }

    /**
     * The page read the metrics; `run`: the read is the one that starts a
     * scenario's run. Once the page was loaded with the query, its run's read
     * means it has the scenario. Any other read changes nothing: the start
     * page's own (at load, at the connection check, at the automatic run's
     * button), and every read before [loaded]: it comes from the bridge's first
     * page, which has no scenario.
     */
    @Synchronized
    fun read(run: Boolean) {
        if (run && loaded) query = null
    }

    companion object {
        /**
         * The handoff of an Activity whose plugin loads. A new Activity takes
         * `extrasQuery` (autorunQuery of its launch extras). A restored one takes
         * `savedPending`, what its saved state kept, and ignores the extras that
         * the system kept in the launch Intent after a process death.
         */
        fun onLoad(extrasQuery: String?, restored: Boolean, savedPending: String?): AutorunHandoff =
            AutorunHandoff(if (restored) savedPending else extrasQuery)
    }
}
