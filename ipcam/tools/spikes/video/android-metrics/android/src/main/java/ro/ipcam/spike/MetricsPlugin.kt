// SPIKE-ONLY (plan 2026-10-18-phase0b-video-spike, Task 10): what the video
// spike reads from Android and production does not need. Never shipped.
package ro.ipcam.spike

import android.os.BatteryManager
import android.os.Build
import android.content.Intent
import android.content.res.Configuration
import android.os.Debug
import android.os.PowerManager
import android.os.Process
import android.util.Log
import android.view.WindowManager
import android.webkit.RenderProcessGoneDetail
import android.webkit.WebView
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.WebViewListener
import com.getcapacitor.annotation.CapacitorPlugin
import java.net.URLEncoder

private const val TAG = "SpikeMetrics"

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
 * Only a cold start honours the extras: `load()` reads them when the Bridge
 * is created and removes them from the Activity's Intent, so a Bridge created
 * again from the same Intent in this process does not start the scenario
 * again (the system hands a process that died a fresh copy of the Intent), and
 * a warm start (`onNewIntent`) does not read them at all. That makes a
 * recreated Activity lose the run, so the manifest's `configChanges` keep the
 * Activity from being recreated by a configuration change. The Activity is exported
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

    override fun load() {
        val intent = activity.intent
        val autorun = intent.getStringExtra("autorun")
        val hub = intent.getStringExtra("hub")
        val query = autorunQuery(autorun, hub)
        intent.removeExtra("autorun")
        intent.removeExtra("hub")
        Log.i(
            TAG,
            "load: pid=${Process.myPid()} activity=${id(activity)} bridge=${id(bridge)} intent=${id(intent)} " +
                "autorun=$autorun hub=${hub != null} config=${configForLog(activity.resources.configuration)}",
        )
        // Runs after the bridge has started its own first load of the page.
        val webView = bridge.webView
        webView.post {
            bridge.addWebViewListener(pageEvents)
            Log.i(TAG, "load posted: attached=${webView.isAttachedToWindow} url=${pageForLog(webView.url)}")
            if (query != null) {
                val url = "${bridge.localUrl}/?$query"
                webView.loadUrl(url)
                Log.i(TAG, "loadUrl: ${pageForLog(url)}")
            }
        }
    }

    private fun lifecycle(event: String, config: Configuration? = null) {
        Log.i(
            TAG,
            "$event: activity=${id(activity)} changingConfigurations=${activity.isChangingConfigurations}" +
                (config?.let { " config=${configForLog(it)}" } ?: ""),
        )
    }

    override fun handleOnStart() = lifecycle("start")

    override fun handleOnResume() = lifecycle("resume")

    override fun handleOnPause() = lifecycle("pause")

    override fun handleOnStop() = lifecycle("stop")

    override fun handleOnDestroy() = lifecycle("destroy", activity.resources.configuration)

    override fun handleOnNewIntent(intent: Intent) {
        Log.i(TAG, "new intent: autorun=${intent.hasExtra("autorun")} (a warm start ignores the extras)")
    }

    override fun handleOnConfigurationChanged(newConfig: Configuration) = lifecycle("configuration changed", newConfig)

    @PluginMethod
    fun read(call: PluginCall) {
        val metrics = JSObject()
        val power = context.getSystemService(PowerManager::class.java)
        power?.let { thermalName(it.currentThermalStatus) }?.let { metrics.put("thermal", it) }
        val battery = context.getSystemService(BatteryManager::class.java)
        battery?.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY)?.takeIf { it in 0..100 }?.let { metrics.put("batteryPct", it) }
        // PSS of this process in KiB. The WebView renderer is a process of its own and is not in it.
        metrics.put("memMb", Debug.getPss() / 1024.0)
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
