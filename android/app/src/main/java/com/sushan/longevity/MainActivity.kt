package com.sushan.longevity

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.health.connect.client.PermissionController
import com.sushan.longevity.sync.HealthConnectReader
import com.sushan.longevity.sync.SyncWorker
import com.sushan.longevity.ui.DashboardScreen

class MainActivity : ComponentActivity() {

    private val reader by lazy { HealthConnectReader(this) }

    private val permissionLauncher =
        registerForActivityResult(PermissionController.createRequestPermissionResultContract()) {
            SyncWorker.schedule(this) // start syncing once access is granted
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        permissionLauncher.launch(reader.permissions)
        SyncWorker.schedule(this)
        setContent { DashboardScreen() }
    }
}
