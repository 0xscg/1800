package com.sushan.longevity

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.health.connect.client.HealthConnectClient
import androidx.health.connect.client.PermissionController
import androidx.lifecycle.lifecycleScope
import com.sushan.longevity.sync.HealthConnectReader
import com.sushan.longevity.sync.SyncWorker
import com.sushan.longevity.ui.DashboardScreen
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {

    private val reader by lazy { HealthConnectReader(this) }

    private val permissionLauncher =
        registerForActivityResult(PermissionController.createRequestPermissionResultContract()) { granted ->
            // Kick off an immediate sync only once access is actually granted.
            if (granted.containsAll(reader.permissions)) SyncWorker.syncNow(this)
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // Periodic worker is safe to schedule regardless: it skips quietly when
        // Health Connect is unavailable or access hasn't been granted yet.
        SyncWorker.schedule(this)
        if (HealthConnectClient.getSdkStatus(this) == HealthConnectClient.SDK_AVAILABLE) {
            lifecycleScope.launch {
                val granted = reader.client().permissionController.getGrantedPermissions()
                if (!granted.containsAll(reader.permissions)) {
                    permissionLauncher.launch(reader.permissions)
                }
            }
        }
        setContent { DashboardScreen() }
    }
}
