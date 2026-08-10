plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    namespace = "au.com.shipper"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        // Reverse DNS of shipper.com.au. Provisional until X-3 registers it in the Play
        // Console: an application id cannot be changed once anything is published under it.
        applicationId = "au.com.shipper"

        // API 24 (Android 7.0), decided in Docs/07 §9. Deliberately above Flutter's own floor
        // and above flutter_secure_storage's EncryptedSharedPreferences requirement of API 23
        // rather than sitting on it — Docs/07 §3 puts the refresh token in the Keystore, and on
        // API 21–22 that storage falls back to something weaker. A token store that is only
        // sometimes hardware-backed is not the guarantee that document makes.
        //
        // A deployment target, not a test target: development runs on a current emulator.
        minSdk = 24
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
