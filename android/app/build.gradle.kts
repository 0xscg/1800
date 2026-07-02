plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

android {
    namespace = "com.sushan.longevity"
    compileSdk = 35
    defaultConfig {
        applicationId = "com.sushan.longevity"
        minSdk = 28 // Health Connect ships as an APK on 9-13, built into Android 14+
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"
        // Configurable via gradle.properties or -PapiBase=... / -PingestToken=...
        // Default reaches the host backend from the emulator.
        val apiBase = (project.findProperty("apiBase") as String?) ?: "http://10.0.2.2:8080"
        val ingestToken = (project.findProperty("ingestToken") as String?) ?: "change-me"
        buildConfigField("String", "API_BASE", "\"$apiBase\"")
        buildConfigField("String", "INGEST_TOKEN", "\"$ingestToken\"")
    }
    buildFeatures { compose = true; buildConfig = true }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
}

dependencies {
    val composeBom = platform("androidx.compose:compose-bom:2024.09.03")
    implementation(composeBom)
    implementation("androidx.activity:activity-compose:1.9.2")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.6")

    // Health Connect — the Android-side equivalent of HealthKit
    implementation("androidx.health.connect:connect-client:1.1.0-alpha10")

    // Background sync
    implementation("androidx.work:work-runtime-ktx:2.9.1")

    // Networking (kept minimal on purpose)
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.7.3")

    // Charts
    implementation("com.patrykandpatrick.vico:compose-m3:2.0.0-beta.1")
}
