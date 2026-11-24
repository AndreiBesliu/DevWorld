package ro.ipcam.spike

import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SpikeUpdateTest {
    private val base = "https://github.com/AndreiBesliu/DevWorld/releases/download"

    @Test
    fun isUpdateUrl_acceptsOnlyTheApkOfThisRepositorysReleases() {
        assertTrue(isUpdateUrl("$base/spike-video-v0.1.2/ipcam-spike.apk"))
        assertTrue(isUpdateUrl("https://github.com/andreibesliu/devworld/releases/download/spike-video-v0.1.12/ipcam-spike.apk"))
        assertFalse(isUpdateUrl("http://github.com/AndreiBesliu/DevWorld/releases/download/spike-video-v0.1.2/ipcam-spike.apk"))
        assertFalse(isUpdateUrl("$base/spike-video-v0.1.2/ipcam-desktop-setup.exe"))
        assertFalse(isUpdateUrl("$base/spike-recording-v0.1.0/ipcam-spike.apk"))
        assertFalse(isUpdateUrl("https://github.com/someone/DevWorld/releases/download/spike-video-v0.1.2/ipcam-spike.apk"))
        assertFalse(isUpdateUrl("$base/spike-video-v0.1.2/ipcam-spike.apk?x=1"))
    }

    @Test
    fun isSha256_wantsSixtyFourLowercaseHexDigits() {
        assertTrue(isSha256("a".repeat(64)))
        assertFalse(isSha256("A".repeat(64)))
        assertFalse(isSha256("a".repeat(63)))
    }

    @Test
    fun copyHashing_copiesAndAnswersTheSha256() {
        val out = ByteArrayOutputStream()
        val hash = copyHashing(ByteArrayInputStream("abc".toByteArray()), out)
        assertEquals("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", hash)
        assertArrayEquals("abc".toByteArray(), out.toByteArray())
    }
}
