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
 * the first thing a scenario does ([handed]). From then on a restored Activity
 * runs nothing: the scenario ran, or was cut, with the old page, and the next
 * `am start` runs it again. The plugin's calls come from the UI thread and
 * from the plugin's own thread.
 */
internal class AutorunHandoff private constructor(private var query: String?) {
    /** The page was loaded with the query, and the next read is its scenario's. */
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
     * The page read the metrics: once it was loaded with the query, that read is
     * its run's, and it has the scenario. A read before is the bridge's first
     * page's, which has no scenario, and changes nothing.
     */
    @Synchronized
    fun handed() {
        if (loaded) query = null
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
