package com.sushan.longevity.sync

import android.content.Context
import androidx.health.connect.client.HealthConnectClient
import androidx.health.connect.client.permission.HealthPermission
import androidx.health.connect.client.records.ActiveCaloriesBurnedRecord
import androidx.health.connect.client.records.HeartRateVariabilityRmssdRecord
import androidx.health.connect.client.records.RestingHeartRateRecord
import androidx.health.connect.client.records.SleepSessionRecord
import androidx.health.connect.client.records.StepsRecord
import androidx.health.connect.client.request.AggregateRequest
import androidx.health.connect.client.request.ReadRecordsRequest
import androidx.health.connect.client.time.TimeRangeFilter
import java.time.Duration
import java.time.LocalDate
import java.time.ZoneId

/** One daily aggregate ready to POST to /v1/ingest/samples. */
data class DailySample(val day: String, val metric: String, val value: Double)

/**
 * Reads the last [days] days from Health Connect and pre-aggregates to daily values.
 * The server stays dumb; the phone owns device quirks.
 */
class HealthConnectReader(private val context: Context) {

    val permissions = setOf(
        HealthPermission.getReadPermission(StepsRecord::class),
        HealthPermission.getReadPermission(HeartRateVariabilityRmssdRecord::class),
        HealthPermission.getReadPermission(RestingHeartRateRecord::class),
        HealthPermission.getReadPermission(SleepSessionRecord::class),
        HealthPermission.getReadPermission(ActiveCaloriesBurnedRecord::class),
    )

    fun client(): HealthConnectClient = HealthConnectClient.getOrCreate(context)

    suspend fun readDailySamples(days: Long = 7): List<DailySample> {
        val hc = client()
        val zone = ZoneId.systemDefault()
        val out = mutableListOf<DailySample>()

        for (offset in 0 until days) {
            val day = LocalDate.now(zone).minusDays(offset)
            val start = day.atStartOfDay(zone).toInstant()
            val end = day.plusDays(1).atStartOfDay(zone).toInstant()
            val range = TimeRangeFilter.between(start, end)
            val dayStr = day.toString()

            // Steps + active energy via the aggregate API (dedupes overlapping sources for us)
            val agg = hc.aggregate(
                AggregateRequest(
                    metrics = setOf(
                        StepsRecord.COUNT_TOTAL,
                        ActiveCaloriesBurnedRecord.ACTIVE_CALORIES_TOTAL,
                    ),
                    timeRangeFilter = range,
                )
            )
            agg[StepsRecord.COUNT_TOTAL]?.let {
                out += DailySample(dayStr, "steps", it.toDouble())
            }
            agg[ActiveCaloriesBurnedRecord.ACTIVE_CALORIES_TOTAL]?.let {
                out += DailySample(dayStr, "active_kcal", it.inKilocalories)
            }

            // HRV rMSSD: average the overnight readings
            val hrv = hc.readRecords(
                ReadRecordsRequest(HeartRateVariabilityRmssdRecord::class, range)
            ).records
            if (hrv.isNotEmpty()) {
                out += DailySample(
                    dayStr, "hrv_rmssd_ms",
                    hrv.map { it.heartRateVariabilityMillis }.average(),
                )
            }

            // Resting HR: take the day's minimum reported value
            val rhr = hc.readRecords(
                ReadRecordsRequest(RestingHeartRateRecord::class, range)
            ).records
            rhr.minOfOrNull { it.beatsPerMinute }?.let {
                out += DailySample(dayStr, "resting_hr", it.toDouble())
            }

            // Sleep sessions ending today -> minutes asleep
            val sleep = hc.readRecords(
                ReadRecordsRequest(SleepSessionRecord::class, range)
            ).records
            val sleepMin = sleep.sumOf { Duration.between(it.startTime, it.endTime).toMinutes() }
            if (sleepMin > 0) out += DailySample(dayStr, "sleep_min", sleepMin.toDouble())
        }
        return out
    }
}
