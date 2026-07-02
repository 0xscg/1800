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

/** Non-2xx ingest response; lets callers decide between retry and give-up. */
class HttpException(val code: Int) : Exception("HTTP $code")

object Api {
    private val http = OkHttpClient()
    private val json = Json { ignoreUnknownKeys = true }
    private val jsonMedia = "application/json".toMediaType()

    /** Returns null when the backend is unreachable or replies with garbage. */
    suspend fun today(): List<MetricToday>? = withContext(Dispatchers.IO) {
        try {
            val req = Request.Builder().url("${BuildConfig.API_BASE}/v1/dashboard/today").build()
            http.newCall(req).execute().use { res ->
                if (!res.isSuccessful) return@withContext null
                json.decodeFromString<List<MetricToday>>(res.body!!.string())
            }
        } catch (e: java.io.IOException) {
            null
        } catch (e: kotlinx.serialization.SerializationException) {
            null
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
            http.newCall(req).execute().use { res ->
                // Surface failures with the status code so the worker can decide
                // between retry (5xx/network) and give-up (4xx).
                if (!res.isSuccessful) throw HttpException(res.code)
            }
        }
}
