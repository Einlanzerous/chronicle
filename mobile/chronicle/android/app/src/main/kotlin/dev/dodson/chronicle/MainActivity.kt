package dev.dodson.chronicle

import dev.dodson.chronicle.capture.CaptureChannel
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine

class MainActivity : FlutterActivity() {
    private var capture: CaptureChannel? = null

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        // The only platform surface this app has. Everything it exposes is
        // something Dart cannot know on its own: whether the microphone is
        // actually open, how much space is left, and whether a live recorder
        // still holds a capture.
        capture = CaptureChannel(this).also {
            it.attach(flutterEngine.dartExecutor.binaryMessenger)
        }
    }

    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray,
    ) {
        // Forwarded before super so the channel's pending result is completed;
        // Android tells the Activity and nothing else.
        if (capture?.onRequestPermissionsResult(requestCode) != true) {
            super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        }
    }
}
