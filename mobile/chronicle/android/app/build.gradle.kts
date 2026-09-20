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
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "dev.dodson.chronicle"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
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
        release {
            // TODO: Add your own signing config for the release build.
            // Signing with the debug keys for now, so `flutter run --release` works.
            signingConfig = signingConfigs.getByName("debug")
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
