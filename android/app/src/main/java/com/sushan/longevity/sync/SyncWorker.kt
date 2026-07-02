package com.sushan.longevity.sync

import android.content.Context
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.health.connect.client.HealthConnectClient
import androidx.work.WorkerParameters
import com.sushan.longevity.data.Api
import com.sushan.longevity.data.HttpException
import java.io.IOException
import java.time.Duration
import java.time.LocalDate
import kotlinx.coroutines.CancellationException

/**
 * Background sync: read Health Connect, POST daily aggregates.
 * Idempotent end-to-end: the batch_id is stable per calendar day, and the server
 * upserts on (day, metric, source) — re-sending the last 7 days is harmless.
 */
class SyncWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {

    override suspend fun doWork(): Result {
        // Permanent conditions: no Health Connect, or access not granted. Skip
        // quietly instead of retrying forever — MainActivity re-asks on launch.
        if (HealthConnectClient.getSdkStatus(applicationContext) != HealthConnectClient.SDK_AVAILABLE) {
            return Result.success()
        }
        val reader = HealthConnectReader(applicationContext)
        val granted = reader.client().permissionController.getGrantedPermissions()
        if (!granted.containsAll(reader.permissions)) return Result.success()

        return try {
            val samples = reader.readDailySamples(days = 7)
            if (samples.isNotEmpty()) {
                // Stable, date-based batch id: every run on the same day resends the
                // same logical batch, so retries and re-syncs stay idempotent.
                Api.postSamples(batchId = "hc-${LocalDate.now()}", samples = samples)
            }
            Result.success()
        } catch (e: CancellationException) {
            throw e // let the coroutine machinery handle stops
        } catch (e: HttpException) {
            // 4xx is our bug (bad payload/auth) — retrying won't fix it.
            if (e.code in 400..499) Result.failure() else Result.retry()
        } catch (e: IOException) {
            Result.retry() // transient network trouble
        } catch (e: Exception) {
            // Never log health values; the exception path carries none.
            Result.retry()
        }
    }

    companion object {
        private val network = Constraints.Builder()
            .setRequiredNetworkType(NetworkType.CONNECTED)
            .build()

        fun schedule(context: Context) {
            val request = PeriodicWorkRequestBuilder<SyncWorker>(Duration.ofHours(6))
                .setConstraints(network)
                .build()
            WorkManager.getInstance(context).enqueueUniquePeriodicWork(
                "health-sync", ExistingPeriodicWorkPolicy.KEEP, request,
            )
        }

        /** Manual "Sync now" from the dashboard. */
        fun syncNow(context: Context) {
            val request = OneTimeWorkRequestBuilder<SyncWorker>()
                .setConstraints(network)
                .build()
            WorkManager.getInstance(context).enqueueUniqueWork(
                "health-sync-now", ExistingWorkPolicy.REPLACE, request,
            )
        }
    }
}
