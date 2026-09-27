plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    namespace = "dev.dodson.chronicle"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        // The released app's identity, and the one the installed debug build
        // from before CHRN-125 also carries -- which is why the first release
        // can update it in place rather than replace it (see signing/ and
        // tool/release_lib.sh). tool/build_release.sh asserts the built APK
        // says exactly this, so a release can never ship under another id and
        // install BESIDE the real app instead of over it.
        applicationId = "dev.dodson.chronicle"
        manifestPlaceholders["appLabel"] = "Chronicle"
        // CHRN-60 sets this explicitly rather than taking Flutter's default of
        // 24. API 29 is where MediaRecorder gained the Ogg container and the
        // Opus encoder, and where MediaRecorder began implementing
        // AudioRecordingMonitor -- the signal that says whether the microphone
        // is actually open rather than merely started. API 30 is where
        // FOREGROUND_SERVICE_TYPE_MICROPHONE arrived, and background microphone
        // access REQUIRES that type. So 29 would be a floor whose behaviour this
        // app never implements and could not test; 30 is the first version whose
        // rules are the ones the capture service actually follows.
        minSdk = 30
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    buildTypes {
        // A debug build is a DIFFERENT app: `dev.dodson.chronicle.dev`, labelled
        // "Chronicle dev", installed beside the real one (CHRN-125).
        //
        // Two reasons, and the first is not optional. The released app is
        // signed by the release key through a rotation lineage whose oldest
        // signer is this machine's debug key, and Android refuses to let a
        // debug-key APK roll that back (INSTALL_FAILED_UPDATE_INCOMPATIBLE,
        // measured on the device) -- so under the released id, `flutter install
        // --debug` simply fails. The second is the one it protects: a debug build
        // that DID land on the real id would share the real app's captures and
        // session, and a device pass would send test memos from them. The
        // suffix also keeps `run-as`, which the non-debuggable release refuses.
        debug {
            applicationIdSuffix = ".dev"
            manifestPlaceholders["appLabel"] = "Chronicle dev"
        }
        release {
            // Gradle signs with the debug key and tool/release_lib.sh REPLACES
            // that signature: `apksigner sign` with the release key and
            // android/signing/lineage.bin. AGP cannot declare a lineage, so the
            // signing that matters happens after Gradle, and there is no
            // key.properties. An APK that skipped the re-sign fails the release
            // job's signer assertion rather than shipping debug-signed.
            signingConfig = signingConfigs.getByName("debug")

            // R8 OFF, explicitly. AGP 9 flipped isMinifyEnabled to default-true
            // for release, and Argosy's first release APK then crashed on launch
            // ("Unable to get provider androidx.startup.InitializationProvider",
            // ARGY-114) with the same startup/WorkManager/Tink path this app
            // pulls in through workmanager and flutter_secure_storage. Lyceum
            // opts out for the same reason. A scratch Chronicle release with R8
            // on DID start on the device -- but only while the phone was asleep,
            // so the UI and Tink never ran -- and after the key rotation there is
            // no rolling back to a debug build, so the first release ships the
            // configuration the estate already knows works. Turning R8 on is its
            // own change, with its own device launch.
            isMinifyEnabled = false
            isShrinkResources = false
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}
