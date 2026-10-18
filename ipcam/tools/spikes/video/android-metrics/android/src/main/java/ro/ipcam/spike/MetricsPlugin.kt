// SPIKE-ONLY (plan 2026-10-18-phase0b-video-spike, Task 10): what the video
// spike reads from Android and production does not need. Never shipped.
package ro.ipcam.spike

import android.os.BatteryManager
import android.os.Build
import android.os.Debug
import android.os.PowerManager
import android.view.WindowManager
import android.webkit.WebView
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import java.net.URLEncoder

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
 * again from the same Intent does not start the scenario again, and a warm
 * start (`onNewIntent`) does not read them at all. The Activity is exported
 * and takes any `hub` URL: any app on the device can start a run against a
 * hub of its choosing. That is by design for a spike that is never published
 * (SPIKE-ONLY); the results hold no addresses or serial numbers (PH0-22).
 */
@CapacitorPlugin(name = "SpikeMetrics")
class MetricsPlugin : Plugin() {
    override fun load() {
        val intent = activity.intent
        val query = autorunQuery(intent.getStringExtra("autorun"), intent.getStringExtra("hub"))
        intent.removeExtra("autorun")
        intent.removeExtra("hub")
        if (query == null) return
        // Runs after the bridge has started its own first load of the page.
        val webView = bridge.webView
        webView.post { webView.loadUrl("${bridge.localUrl}/?$query") }
    }

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
