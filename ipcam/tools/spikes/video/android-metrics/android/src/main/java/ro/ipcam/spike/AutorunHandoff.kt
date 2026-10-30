// SPIKE-ONLY (plan 2026-10-18-phase0b-video-spike, Task 10): what the video
// spike reads from Android and production does not need. Never shipped.
package ro.ipcam.spike

/**
 * The launch extras' scenario (R5) between the Activity's creation and the
 * page that runs it. The Activity's saved state keeps [pendingQuery], so a
 * scenario survives the Activity being created again before its page got the
 * query: the system can do that right after a launch, for instance when it
 * changes the app's resources (overlays) after the device booted, and no
 * `configChanges` entry stops it. Once the page has the query ([handed]), a
 * restored Activity runs nothing: the scenario ran, or was cut, with the old
 * page, and the next `am start` runs it again.
 */
internal class AutorunHandoff private constructor(query: String?) {
    /** The query string that the page has not been given yet, or null. */
    var pendingQuery: String? = query
        private set

    /** The page was given the query. */
    fun handed() {
        pendingQuery = null
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
