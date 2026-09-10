package com.yoyopaste

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.*
import java.net.HttpURLConnection
import java.net.URL
import org.json.JSONArray

data class Peer(val id: String, val name: String, val os: String, val ip: String, val online: Boolean)

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            var peers by remember { mutableStateOf(listOf<Peer>()) }
            var enabled by remember { mutableStateOf(true) }
            LaunchedEffect(Unit) {
                peers = fetchPeers()
            }
            Column(modifier = Modifier.padding(16.dp)) {
                Row { Text("YoYoPaste", style = MaterialTheme.typography.headlineSmall); Spacer(Modifier.weight(1f)); Switch(checked = enabled, onCheckedChange = { enabled = it }) }
                Spacer(Modifier.height(16.dp))
                peers.forEach { p ->
                    Row(modifier = Modifier.fillMaxWidth()) {
                        Text(p.name, modifier = Modifier.weight(1f))
                        Text(p.ip)
                        Text(if (p.online) "online" else "offline")
                    }
                }
                Text("Auto-sync is desktop only (Android 10+ blocks background clipboard)", style = MaterialTheme.typography.bodySmall)
            }
        }
    }
    suspend fun fetchPeers(): List<Peer> = withContext(Dispatchers.IO) {
        val addr = getSharedPreferences("yoyopaste", MODE_PRIVATE).getString("bootstrapAddr", "100.64.0.1:8383")!!
        try {
            val url = URL("http://$addr/v0/peers")
            val conn = url.openConnection() as HttpURLConnection
            conn.connectTimeout = 2000
            conn.readTimeout = 2000
            conn.requestMethod = "GET"
            if (conn.responseCode != 200) return@withContext emptyList()
            val text = conn.inputStream.bufferedReader().readText()
            val arr = JSONArray(text)
            (0 until arr.length()).map { i ->
                val o = arr.getJSONObject(i)
                Peer(o.getString("id"), o.getString("name"), o.getString("os"), o.getString("ip"), o.getBoolean("online"))
            }
        } catch (e: Exception) { emptyList() }
    }
}
