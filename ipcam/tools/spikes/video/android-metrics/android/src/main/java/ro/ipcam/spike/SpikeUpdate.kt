// SPIKE-ONLY (owner's request of 2026-11-23): the test APK's own updates
// (update.ts of the spike app). The APK of a spike-video-v* release is
// downloaded from this repository's GitHub releases and checked against the
// SHA-256 that GitHub lists for it; Android then asks the person to confirm
// the install. TODO (Phase 4): the product updates through the Play Store.
package ro.ipcam.spike

import java.io.File
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest

private val UPDATE_URL = Regex(
    "https://github\\.com/AndreiBesliu/DevWorld/releases/download/spike-video-v[0-9]+\\.[0-9]+\\.[0-9]+/ipcam-spike\\.apk",
    RegexOption.IGNORE_CASE,
)

private val SHA256_HEX = Regex("[0-9a-f]{64}")

/** Whether `url` is the APK of a release of this app: the shell downloads nothing else. */
internal fun isUpdateUrl(url: String): Boolean = UPDATE_URL.matches(url)

/** Whether `sha256` is a SHA-256 as the page sends it: 64 lowercase hex digits. */
internal fun isSha256(sha256: String): Boolean = SHA256_HEX.matches(sha256)

/** Copies `input` to `output` and answers the SHA-256 of what it copied, in lowercase hex. */
internal fun copyHashing(input: InputStream, output: OutputStream): String {
    val digest = MessageDigest.getInstance("SHA-256")
    val buffer = ByteArray(64 * 1024)
    while (true) {
        val n = input.read(buffer)
        if (n < 0) break
        digest.update(buffer, 0, n)
        output.write(buffer, 0, n)
    }
    return digest.digest().joinToString("") { "%02x".format(it) }
}

/**
 * Downloads the release APK at `url` into `dir` and answers its file, or throws
 * when the download fails or its SHA-256 is not `sha256` (the file is deleted).
 * GitHub redirects the download to its storage host, over HTTPS too.
 */
internal fun downloadVerified(url: String, sha256: String, dir: File): File {
    require(isUpdateUrl(url) && isSha256(sha256)) { "not a release APK of this app" }
    dir.mkdirs()
    val file = File(dir, "ipcam-spike.apk")
    val connection = URL(url).openConnection() as HttpURLConnection
    try {
        connection.instanceFollowRedirects = true
        connection.connectTimeout = 30_000
        connection.readTimeout = 60_000
        if (connection.responseCode != HttpURLConnection.HTTP_OK) {
            throw IOException("GitHub answered ${connection.responseCode}")
        }
        val actual = connection.inputStream.use { input -> file.outputStream().use { copyHashing(input, it) } }
        if (actual != sha256) {
            file.delete()
            throw IOException("the download's SHA-256 is not the release's")
        }
    } finally {
        connection.disconnect()
    }
    return file
}
