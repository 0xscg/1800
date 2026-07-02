package com.sushan.longevity.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.sushan.longevity.data.Api
import com.sushan.longevity.data.MetricToday
import kotlin.math.abs

// Same tokens as the web app — one visual language across surfaces.
private val Ink = Color(0xFF0C1116)
private val Panel = Color(0xFF131A21)
private val PanelEdge = Color(0xFF1E2731)
private val Text = Color(0xFFE7ECF0)
private val TextDim = Color(0xFF8B98A5)
private val Sage = Color(0xFF8FBCA8)
private val Ember = Color(0xFFE0785C)
private val Band = Color(0xFF22303B)

private val HIGHER_IS_BETTER = mapOf(
    "hrv_rmssd_ms" to true, "resting_hr" to false, "sleep_min" to true,
    "sleep_efficiency" to true, "recovery_score" to true, "steps" to true,
    "active_kcal" to true, "vo2max" to true, "respiratory_rate" to null,
)

private val LABELS = mapOf(
    "hrv_rmssd_ms" to "HRV", "resting_hr" to "Resting HR", "sleep_min" to "Sleep",
    "sleep_efficiency" to "Sleep eff.", "recovery_score" to "Recovery",
    "respiratory_rate" to "Resp. rate", "steps" to "Steps",
    "active_kcal" to "Active kcal", "vo2max" to "VO₂max",
)

private fun zColor(z: Double?, dir: Boolean?): Color = when {
    z == null || abs(z) < 0.75 -> TextDim
    dir == null -> Ember
    (z > 0) == dir -> Sage
    else -> Ember
}

@Composable
fun DashboardScreen() {
    var metrics by remember { mutableStateOf<List<MetricToday>>(emptyList()) }

    LaunchedEffect(Unit) { metrics = Api.today() }

    Column(
        Modifier.fillMaxSize().background(Ink).padding(20.dp)
    ) {
        Row(
            Modifier.fillMaxWidth().padding(bottom = 20.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            androidx.compose.material3.Text(
                "Baseline", color = Text, fontSize = 22.sp, fontWeight = FontWeight.Medium,
            )
            androidx.compose.material3.Text(
                "you vs. you", color = TextDim, fontSize = 13.sp, fontFamily = FontFamily.Monospace,
            )
        }

        if (metrics.isEmpty()) {
            androidx.compose.material3.Text(
                "No data yet. Grant Health Connect access and let the first sync run.",
                color = TextDim,
            )
        }

        LazyVerticalGrid(
            columns = GridCells.Adaptive(minSize = 160.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            items(metrics) { m -> MetricCard(m) }
        }
    }
}

@Composable
private fun MetricCard(m: MetricToday) {
    val dir = HIGHER_IS_BETTER[m.metric]
    Column(
        Modifier
            .background(Panel, RoundedCornerShape(10.dp))
            .padding(14.dp)
            .fillMaxWidth()
    ) {
        androidx.compose.material3.Text(
            (LABELS[m.metric] ?: m.metric).uppercase(),
            color = TextDim, fontSize = 11.sp, letterSpacing = 1.sp,
        )
        Spacer(Modifier.height(6.dp))
        androidx.compose.material3.Text(
            "%.0f".format(m.value),
            color = Text, fontSize = 30.sp,
            fontFamily = FontFamily.Monospace, fontWeight = FontWeight.SemiBold,
        )
        m.z?.let {
            androidx.compose.material3.Text(
                "${if (it >= 0) "+" else ""}%.1fσ vs 30d".format(it),
                color = zColor(it, dir), fontSize = 12.sp, fontFamily = FontFamily.Monospace,
            )
        }
        Spacer(Modifier.height(10.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            val recent = m.spark.takeLast(14)
            repeat(14) { i ->
                val z = recent.getOrNull(i)
                Box(
                    Modifier.size(8.dp).background(
                        if (z == null) Band else zColor(z, dir).copy(
                            alpha = if (abs(z) < 0.75) 0.35f else 1f
                        ),
                        CircleShape,
                    )
                )
            }
        }
    }
}
