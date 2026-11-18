package ro.ipcam.pip

import android.util.Rational
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [36])
class PipPluginTest {
    @Test
    fun aspectRatio16by9() {
        assertEquals(Rational(16, 9), pipParams().aspectRatio)
    }
}
