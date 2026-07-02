package com.sushan.longevity.data

import com.sushan.longevity.BuildConfig
import com.sushan.longevity.sync.DailySample
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

@Serializable
data class MetricToday(
    val metric: String,
    val day: String,
    val value: Double,
    val mean30: Double? = null,
    val sd30: Double? = null,
    val z: Double? = null,
    val spark: List<Double> = emptyList(),
)

@Serializable
private data class SamplePayload(val day: String, val metric: String, val value: Double)

@Serializable
private data class IngestBody(val batch_id: String, val samples: List<SamplePayload>)

object Api {
    private val http = OkHttpClient()
    private val json = Json { ignoreUnknownKeys = true }
    private val jsonMedia = "application/json".toMediaType()

    suspend fun today(): List<MetricToday> = withContext(Dispatchers.IO) {
        val req = Request.Builder().url("${BuildConfig.API_BASE}/v1/dashboard/today").build()
        http.newCall(req).execute().use { res ->
            if (!res.isSuccessful) return@withContext emptyList()
            json.decodeFromString(res.body!!.string())
        }
    }

    suspend fun postSamples(batchId: String, samples: List<DailySample>) =
        withContext(Dispatchers.IO) {
            val body = IngestBody(
                batch_id = batchId,
                samples = samples.map { SamplePayload(it.day, it.metric, it.value) },
            )
            val req = Request.Builder()
                .url("${BuildConfig.API_BASE}/v1/ingest/samples")
                .header("Authorization", "Bearer ${BuildConfig.INGEST_TOKEN}")
                .post(json.encodeToString(body).toRequestBody(jsonMedia))
                .build()
            http.newCall(req).execute().close()
        }
}
