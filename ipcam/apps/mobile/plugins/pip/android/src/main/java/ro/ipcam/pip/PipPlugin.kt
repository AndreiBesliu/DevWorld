package ro.ipcam.pip

import android.app.PictureInPictureParams
import android.content.pm.PackageManager
import android.util.Rational
import androidx.core.app.PictureInPictureModeChangedInfo
import androidx.core.util.Consumer
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin

/** Picture-in-picture of the whole Activity at 16:9, the shape of a camera picture (spec §6.6). */
internal fun pipParams(): PictureInPictureParams =
    PictureInPictureParams.Builder().setAspectRatio(Rational(16, 9)).build()

/**
 * The `pip` plugin: `enter()` puts the Activity into picture-in-picture, and the
 * `pipChanged` event ({ on }) tells the page when it enters and leaves it. The
 * app's manifest declares android:supportsPictureInPicture on the Activity.
 */
@CapacitorPlugin(name = "Pip")
class PipPlugin : Plugin() {
    private val modeChanged = Consumer<PictureInPictureModeChangedInfo> { info ->
        notifyListeners("pipChanged", JSObject().put("on", info.isInPictureInPictureMode))
    }

    override fun load() {
        activity.addOnPictureInPictureModeChangedListener(modeChanged)
    }

    override fun handleOnDestroy() {
        activity.removeOnPictureInPictureModeChangedListener(modeChanged)
    }

    @PluginMethod
    fun enter(call: PluginCall) {
        if (!activity.packageManager.hasSystemFeature(PackageManager.FEATURE_PICTURE_IN_PICTURE)) {
            call.unavailable("This device has no picture-in-picture.")
            return
        }
        activity.runOnUiThread {
            try {
                if (activity.enterPictureInPictureMode(pipParams())) call.resolve() else call.reject("Picture-in-picture was refused.")
            } catch (e: IllegalStateException) {
                // The Activity does not declare android:supportsPictureInPicture.
                call.reject(e.message, e)
            }
        }
    }
}
