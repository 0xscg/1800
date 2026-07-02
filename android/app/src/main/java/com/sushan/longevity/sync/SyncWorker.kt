package com.sushan.longevity.sync

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import com.sushan.longevity.data.Api
import java.time.Duration
import java.util.UUID

/** Periodic background sync: read Health Connect, POST daily aggregates. Idempotent server-side. */
class SyncWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {

    override suspend fun doWork(): Result {
        return try {
            val samples = HealthConnectReader(applicationContext).readDailySamples(days = 7)
            if (samples.isNotEmpty()) {
                Api.postSamples(batchId = UUID.randomUUID().toString(), samples = samples)
            }
            Result.success()
        } catch (e: Exception) {
            Result.retry()
        }
    }

    companion object {
        fun schedule(context: Context) {
            val request = PeriodicWorkRequestBuilder<SyncWorker>(Duration.ofHours(6)).build()
            WorkManager.getInstance(context).enqueueUniquePeriodicWork(
                "health-sync", ExistingPeriodicWorkPolicy.KEEP, request,
            )
        }
    }
}
