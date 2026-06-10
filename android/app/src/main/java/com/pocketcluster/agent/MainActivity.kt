package com.pocketcluster.agent

import android.Manifest
import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.Uri
import android.net.wifi.WifiManager
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.os.PowerManager
import android.os.StatFs
import android.provider.Settings
import android.view.View
import android.widget.Button
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast
import com.pocketcluster.agent.agent.AgentService
import java.io.File
import java.net.Inet4Address
import java.net.NetworkInterface
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

class MainActivity : Activity() {

    private lateinit var tvStatus: TextView
    private lateinit var addressCard: View
    private lateinit var tvAddress: TextView
    private lateinit var tvNodeId: TextView
    private lateinit var tvNodeInfo: TextView
    private lateinit var btnToggle: Button
    private lateinit var btnWebUI: Button
    private lateinit var btnCopy: Button
    private lateinit var statusContainer: LinearLayout

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        tvStatus = findViewById(R.id.tvStatus)
        addressCard = findViewById(R.id.addressCard)
        tvAddress = findViewById(R.id.tvAddress)
        tvNodeId = findViewById(R.id.tvNodeId)
        tvNodeInfo = findViewById(R.id.tvNodeInfo)
        btnToggle = findViewById(R.id.btnToggle)
        btnWebUI = findViewById(R.id.btnWebUI)
        btnCopy = findViewById(R.id.btnCopy)
        statusContainer = findViewById(R.id.statusContainer)

        btnToggle.setOnClickListener { onToggleClicked() }
        btnWebUI.setOnClickListener {
            startActivity(Intent(this, WebUIActivity::class.java))
        }
        btnCopy.setOnClickListener { copyAddress() }

        findViewById<Button>(R.id.btnLogs).setOnClickListener {
            showLogDialog()
        }

        checkBatteryOptimization()
        updateUI()
    }

    override fun onResume() {
        super.onResume()
        updateUI()
    }

    private fun onToggleClicked() {
        if (AgentService.isRunning) {
            stopService(Intent(this, AgentService::class.java))
            btnToggle.postDelayed({ updateUI() }, 500)
        } else {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                if (checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
                    != PackageManager.PERMISSION_GRANTED
                ) {
                    requestPermissions(
                        arrayOf(Manifest.permission.POST_NOTIFICATIONS),
                        REQ_NOTIFICATION
                    )
                    return
                }
            }
            startAgent()
        }
    }

    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray
    ) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == REQ_NOTIFICATION) {
            if (grantResults.isNotEmpty() && grantResults[0] == PackageManager.PERMISSION_GRANTED) {
                startAgent()
            } else {
                Toast.makeText(this, "Notification permission required for foreground service", Toast.LENGTH_LONG).show()
            }
        }
    }

    private fun startAgent() {
        val intent = Intent(this, AgentService::class.java)
        startForegroundService(intent)
        btnToggle.postDelayed({ updateUI() }, 500)
    }

    private fun checkBatteryOptimization() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
            if (!pm.isIgnoringBatteryOptimizations(packageName)) {
                // Show battery optimization prompt
                val promptCard = findViewById<View>(R.id.batteryCard)
                promptCard?.visibility = View.VISIBLE
                findViewById<Button>(R.id.btnBattery)?.setOnClickListener {
                    try {
                        val intent = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS).apply {
                            data = Uri.parse("package:$packageName")
                        }
                        startActivity(intent)
                    } catch (e: Exception) {
                        Toast.makeText(this, "Please disable battery optimization manually", Toast.LENGTH_LONG).show()
                    }
                }
            }
        }
    }

    private fun getWifiIpAddress(): String? {
        try {
            val wifiManager = applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
            val ip = wifiManager.connectionInfo.ipAddress
            if (ip != 0) {
                return String.format("%d.%d.%d.%d",
                    ip and 0xff, ip shr 8 and 0xff, ip shr 16 and 0xff, ip shr 24 and 0xff)
            }
        } catch (_: Exception) {}
        try {
            for (intf in NetworkInterface.getNetworkInterfaces()) {
                if (intf.isLoopback || !intf.isUp) continue
                for (addr in intf.inetAddresses) {
                    if (addr is Inet4Address && !addr.isLoopbackAddress) {
                        return addr.hostAddress
                    }
                }
            }
        } catch (_: Exception) {}
        return null
    }

    private fun copyAddress() {
        val address = tvAddress.text.toString()
        if (address.isNotEmpty()) {
            val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
            clipboard.setPrimaryClip(ClipData.newPlainText("address", address))
            Toast.makeText(this, "Address copied", Toast.LENGTH_SHORT).show()
        }
    }

    private fun updateUI() {
        if (AgentService.isRunning) {
            tvStatus.text = "● Running"
            tvStatus.setTextColor(0xFF4CAF50.toInt())

            val ip = getWifiIpAddress()
            val address = if (ip != null) "http://$ip:7788" else "http://localhost:7788"
            tvAddress.text = address
            tvNodeId.text = "Node: ${AgentService.nodeId ?: "detecting..."}"
            tvNodeInfo.text = "Other nodes can join using this address"
            tvNodeInfo.visibility = View.VISIBLE
            addressCard.visibility = View.VISIBLE
            btnToggle.text = "Stop Agent"
            btnWebUI.visibility = View.VISIBLE
        } else {
            tvStatus.text = "○ Stopped"
            tvStatus.setTextColor(0xFF888888.toInt())
            addressCard.visibility = View.GONE
            tvNodeInfo.visibility = View.GONE
            btnToggle.text = "Start Agent"
            btnWebUI.visibility = View.GONE
        }

        // Update status cards
        updateStatusCards()
    }

    private fun updateStatusCards() {
        statusContainer.removeAllViews()

        // Storage
        val dataDir = File(filesDir, "pocketcluster")
        val storageOk = dataDir.exists() || filesDir.canWrite()
        val storageStat = try { StatFs(filesDir.absolutePath) } catch (_: Exception) { null }
        val freeGB = storageStat?.let { it.availableBlocksLong * it.blockSizeLong / (1024.0 * 1024 * 1024) }
        addStatusCard("Storage", if (storageOk) "ok" else "error",
            if (freeGB != null) String.format(Locale.US, "%.1f GB free", freeGB)
            else if (storageOk) "Writable" else "Not writable")

        // Wi-Fi
        val cm = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val network = cm.activeNetwork
        val caps = network?.let { cm.getNetworkCapabilities(it) }
        val isWifi = caps?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true
        addStatusCard("Network", if (isWifi) "ok" else "warn",
            if (isWifi) "Connected via Wi-Fi" else "Not on Wi-Fi (mDNS may not work)")

        // Battery optimization
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
            val ignoring = pm.isIgnoringBatteryOptimizations(packageName)
            addStatusCard("Battery", if (ignoring) "ok" else "warn",
                if (ignoring) "Optimization disabled" else "May be killed when idle")
        }

        // Agent health
        if (AgentService.isRunning) {
            addStatusCard("Agent", "ok", "Listening on port 7788")
        }
    }

    private fun addStatusCard(title: String, status: String, detail: String) {
        val card = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            setPadding(24, 16, 24, 16)
            setBackgroundResource(R.drawable.card_bg)
            val lp = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
            )
            lp.bottomMargin = 8
            layoutParams = lp
        }

        val icon = TextView(this).apply {
            text = when (status) {
                "ok" -> "✓"
                "warn" -> "⚠"
                else -> "✗"
            }
            textSize = 16f
            setTextColor(when (status) {
                "ok" -> 0xFF4CAF50.toInt()
                "warn" -> 0xFFFF9800.toInt()
                else -> 0xFFF44336.toInt()
            })
            val lp = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.WRAP_CONTENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
            )
            lp.marginEnd = 16
            layoutParams = lp
        }

        val textLayout = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
        }

        val titleView = TextView(this).apply {
            text = title
            textSize = 14f
            setTypeface(null, android.graphics.Typeface.BOLD)
        }

        val detailView = TextView(this).apply {
            text = detail
            textSize = 12f
            setTextColor(0xFF666666.toInt())
        }

        textLayout.addView(titleView)
        textLayout.addView(detailView)
        card.addView(icon)
        card.addView(textLayout)
        statusContainer.addView(card)
    }

    private fun showLogDialog() {
        val logs = AgentService.logLines.toList()
        val logText = if (logs.isEmpty()) "No logs yet." else logs.joinToString("\n")

        val scrollView = ScrollView(this).apply {
            setPadding(32, 32, 32, 32)
        }
        val textView = TextView(this).apply {
            text = logText
            textSize = 11f
            setTypeface(android.graphics.Typeface.MONOSPACE)
            setTextIsSelectable(true)
        }
        scrollView.addView(textView)

        android.app.AlertDialog.Builder(this)
            .setTitle("Agent Logs")
            .setView(scrollView)
            .setPositiveButton("Close", null)
            .setNeutralButton("Copy") { _, _ ->
                val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                clipboard.setPrimaryClip(ClipData.newPlainText("logs", logText))
                Toast.makeText(this, "Logs copied", Toast.LENGTH_SHORT).show()
            }
            .show()
    }

    companion object {
        private const val REQ_NOTIFICATION = 1001
    }
}
