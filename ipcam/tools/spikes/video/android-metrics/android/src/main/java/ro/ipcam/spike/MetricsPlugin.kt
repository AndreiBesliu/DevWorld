// SPIKE-ONLY (plan 2026-10-18-phase0b-video-spike, Task 10): what the video
// spike reads from Android and production does not need. Never shipped.
package ro.ipcam.spike

import android.app.ActivityManager
import android.os.BatteryManager
import android.os.Build
import android.content.Intent
import android.content.IntentFilter
import android.content.res.Configuration
import android.os.Bundle
import android.os.Debug
import android.os.PowerManager
import android.os.Process
import android.util.Log
import android.view.WindowManager
import android.webkit.RenderProcessGoneDetail
import android.webkit.WebView
import androidx.savedstate.SavedStateRegistry
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.WebViewListener
import com.getcapacitor.annotation.CapacitorPlugin
import java.net.URLEncoder

private const val TAG = "SpikeMetrics"

/** The key of the Activity's saved state: this Activity has read its launch extras. */
private const val HANDLED_KEY = "spikeAutorunHandled"

/** The key, in that state, of the query string that its page had not been given yet (AutorunHandoff). */
private const val PENDING_KEY = "spikeAutorunPending"

private val THERMAL_NAMES = mapOf(
    PowerManager.THERMAL_STATUS_NONE to "NONE",
    PowerManager.THERMAL_STATUS_LIGHT to "LIGHT",
    PowerManager.THERMAL_STATUS_MODERATE to "MODERATE",
    PowerManager.THERMAL_STATUS_SEVERE to "SEVERE",
    PowerManager.THERMAL_STATUS_CRITICAL to "CRITICAL",
    PowerManager.THERMAL_STATUS_EMERGENCY to "EMERGENCY",
    PowerManager.THERMAL_STATUS_SHUTDOWN to "SHUTDOWN",
)

/** PowerManager's thermal status by its name in the spike app (THERMAL_LEVELS), or null for a status it does not know. */
internal fun thermalName(status: Int): String? = THERMAL_NAMES[status]

/**
 * Whether the phone is on the charger (R48). `plugged` is EXTRA_PLUGGED of the
 * sticky ACTION_BATTERY_CHANGED intent (null without one) and `isCharging` is
 * BatteryManager.isCharging. Plugged in counts even when the battery does not
 * charge: full, or held at a level by the phone's battery protection. Without
 * the intent, isCharging decides; null when neither is known.
 */
internal fun onCharger(plugged: Int?, isCharging: Boolean?): Boolean? =
    if (plugged != null) plugged != 0 || isCharging == true else isCharging

/**
 * A page address for the log: without its query, which holds the hub's address
 * (PH0-22), but with the scenario an autorun asks for.
 */
internal fun pageForLog(url: String?): String {
    if (url == null) return "none"
    val scenario = Regex("[?&]autorun=([^&#]*)").find(url)?.groupValues?.get(1)
    return url.substringBefore('?').substringBefore('#') + (scenario?.let { " (autorun $it)" } ?: "")
}

private fun id(any: Any?): String = Integer.toHexString(System.identityHashCode(any))

/** The start of Configuration.toString(): the part a configuration change alters (mcc, mnc, locale, size, density). */
private fun configForLog(config: Configuration): String = config.toString().take(150)

/**
 * The spike app's query string for the launch extras `autorun` and `hub` (R5),
 * or null without `autorun`.
 */
internal fun autorunQuery(autorun: String?, hub: String?): String? {
    if (autorun == null) return null
    val params = listOfNotNull("autorun" to autorun, hub?.let { "hub" to it })
    return params.joinToString("&") { (name, value) -> "$name=${URLEncoder.encode(value, "UTF-8")}" }
}

/**
 * `read()` answers the metrics of the spike app's NativeMetrics that Android
 * knows; `keepAwake({ on })` keeps the screen on while a scenario runs. An
 * autorun launch (`adb shell am start ... --es autorun S1 --es hub <url>`)
 * reloads the page with the extras as its query string.
 *
 * Only a cold start honours the extras. `load()` reads them once and removes
 * them from the Activity's Intent, and the Activity's saved state (which the
 * system keeps across a process death) holds the scenario until the page has
 * been given it (AutorunHandoff). An Activity that is created again before that
 * runs the scenario; one restored after its page had it (it was created again,
 * or its process died and the system started it from the launch Intent it
 * kept, extras included) ignores the extras: its scenario ended with the old
 * page, and the next `am start` is the way to run it again. A warm start
 * (`onNewIntent`) does not read them at all.
 * The manifest's `configChanges` keep configuration changes from creating the
 * Activity again, because that would lose the run in progress. The Activity is exported
 * and takes any `hub` URL: any app on the device can start a run against a
 * hub of its choosing. That is by design for a spike that is never published
 * (SPIKE-ONLY); the results hold no addresses or serial numbers (PH0-22).
 */
@CapacitorPlugin(name = "SpikeMetrics")
class MetricsPlugin : Plugin() {
    // What the page and the Activity do, for the emulator job's annotations
    // (ci/emulator-smoke.sh): they show how often the app was created and
    // which address the WebView was given.
    private val pageEvents = object : WebViewListener() {
        override fun onPageStarted(webView: WebView) {
            Log.i(TAG, "page started: ${pageForLog(webView.url)}")
        }

        override fun onPageLoaded(webView: WebView) {
            Log.i(TAG, "page loaded: ${pageForLog(webView.url)}")
        }

        override fun onRenderProcessGone(webView: WebView, detail: RenderProcessGoneDetail): Boolean {
            Log.w(TAG, "render process gone: crashed=${detail.didCrash()}")
            return false // not handled, as without this listener
        }
    }

    /** The launch extras' scenario until the page has it; the Activity's saved state keeps it. */
    private var handoff = AutorunHandoff.onLoad(extrasQuery = null, restored = false, savedPending = null)

    override fun load() {
        val intent = activity.intent
        val registry = activity.savedStateRegistry
        val saved = registry.consumeRestoredStateForKey(HANDLED_KEY)
        val restored = saved != null
        val autorun = intent.getStringExtra("autorun")
        val hub = intent.getStringExtra("hub")
        handoff = AutorunHandoff.onLoad(autorunQuery(autorun, hub), restored, saved?.getString(PENDING_KEY))
        if (registry.getSavedStateProvider(HANDLED_KEY) == null) {
            registry.registerSavedStateProvider(
                HANDLED_KEY,
                SavedStateRegistry.SavedStateProvider {
                    Bundle().apply {
                        putBoolean(HANDLED_KEY, true)
                        putString(PENDING_KEY, handoff.pendingQuery)
                    }
                },
            )
        }
        intent.removeExtra("autorun")
        intent.removeExtra("hub")
        Log.i(
            TAG,
            "load: pid=${Process.myPid()} activity=${id(activity)} bridge=${id(bridge)} intent=${id(intent)} restored=$restored " +
                "autorun=$autorun hub=${hub != null} pending=${saved?.getString(PENDING_KEY) != null} " +
                "run=${handoff.pendingQuery != null} config=${configForLog(activity.resources.configuration)}",
        )
        // Runs after the bridge has started its own first load of the page.
        val webView = bridge.webView
        webView.post {
            bridge.addWebViewListener(pageEvents)
            Log.i(TAG, "load posted: attached=${webView.isAttachedToWindow} url=${pageForLog(webView.url)}")
            handoff.pendingQuery?.let { query ->
                val url = "${bridge.localUrl}/?$query"
                webView.loadUrl(url)
                handoff.handed()
                Log.i(TAG, "loadUrl: ${pageForLog(url)}")
            }
        }
    }

    private fun lifecycle(event: String, config: Configuration? = null, changes: Int? = null) {
        Log.i(
            TAG,
            "$event: activity=${id(activity)} changingConfigurations=${activity.isChangingConfigurations}" +
                (changes?.let { " changes=0x${Integer.toHexString(it)}" } ?: "") +
                (config?.let { " config=${configForLog(it)}" } ?: ""),
        )
    }

    override fun handleOnStart() = lifecycle("start")

    override fun handleOnResume() = lifecycle("resume")

    override fun handleOnPause() = lifecycle("pause")

    override fun handleOnStop() = lifecycle("stop")

    // With the changes that create the Activity again (getChangingConfigurations), for
    // instance 0x80000000 when the system changes the app's resources (CONFIG_ASSETS_PATHS).
    override fun handleOnDestroy() = lifecycle("destroy", activity.resources.configuration, activity.changingConfigurations)

    // BridgeActivity.load() passes the Activity's own launch Intent here too (launch=true),
    // after load() took its extras. Only launch=false is a warm start's Intent.
    override fun handleOnNewIntent(intent: Intent) {
        Log.i(TAG, "new intent: autorun=${intent.hasExtra("autorun")} launch=${intent === activity.intent} (a warm start ignores the extras)")
    }

    override fun handleOnConfigurationChanged(newConfig: Configuration) = lifecycle("configuration changed", newConfig)

    @PluginMethod
    fun read(call: PluginCall) {
        val metrics = JSObject()
        val power = context.getSystemService(PowerManager::class.java)
        power?.let { thermalName(it.currentThermalStatus) }?.let { metrics.put("thermal", it) }
        val battery = context.getSystemService(BatteryManager::class.java)
        battery?.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY)?.takeIf { it in 0..100 }?.let { metrics.put("batteryPct", it) }
        // A phone on the charger does not measure MV5's battery drain (R48). A null receiver only reads the sticky intent.
        val batteryState = context.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val plugged = batteryState?.getIntExtra(BatteryManager.EXTRA_PLUGGED, -1)?.takeIf { it >= 0 }
        onCharger(plugged, battery?.isCharging)?.let { metrics.put("charging", it) }
        // PSS of this process in KiB. The WebView renderer is a process of its own and is not in it.
        metrics.put("memMb", Debug.getPss() / 1024.0)
        // The phone's physical memory, which the report notes for AND-1 (spec §16.3).
        context.getSystemService(ActivityManager::class.java)?.let { activities ->
            val memory = ActivityManager.MemoryInfo().also { activities.getMemoryInfo(it) }
            memory.totalMem.takeIf { it > 0 }?.let { metrics.put("ramMb", it / (1024 * 1024)) }
        }
        metrics.put("model", "${Build.MANUFACTURER} ${Build.MODEL}")
        metrics.put("os", "Android ${Build.VERSION.RELEASE} (API ${Build.VERSION.SDK_INT})")
        WebView.getCurrentWebViewPackage()?.let { metrics.put("webview", "${it.packageName} ${it.versionName}") }
        call.resolve(metrics)
    }

    @PluginMethod
    fun keepAwake(call: PluginCall) {
        val on = call.getBoolean("on") ?: return call.reject("keepAwake needs { on: boolean }")
        activity.runOnUiThread {
            val flag = WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON
            if (on) activity.window.addFlags(flag) else activity.window.clearFlags(flag)
            call.resolve()
        }
    }
}
