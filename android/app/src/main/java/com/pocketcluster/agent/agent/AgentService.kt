package com.pocketcluster.agent.agent

import android.app.Notification
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.net.wifi.WifiManager
import android.os.Build
import android.os.IBinder
import android.os.PowerManager
import android.util.Log
import com.pocketcluster.agent.MainActivity
import com.pocketcluster.agent.PocketClusterApp
import com.pocketcluster.agent.R
import java.io.BufferedReader
import java.io.File
import java.io.InputStreamReader
import java.net.Inet4Address
import java.net.NetworkInterface

class AgentService : Service() {

    companion object {
        private const val TAG = "AgentService"
        private const val NOTIFICATION_ID = 1
        private const val GO_BINARY_NAME = "libpocketcluster.so"
        private const val DEFAULT_PORT = 7788
        var isRunning: Boolean = false
            private set
        var nodeId: String? = null
            private set
    }

    private var process: Process? = null
    private var wakeLock: PowerManager.WakeLock? = null
    private var multicastLock: WifiManager.MulticastLock? = null
    private var logThread: Thread? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        Log.i(TAG, "Agent service created")
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForeground(NOTIFICATION_ID, buildNotification("Starting Go agent..."))
        startAgent()
        return START_STICKY
    }

    override fun onDestroy() {
        stopAgent()
        super.onDestroy()
    }

    private fun startAgent() {
        try {
            val binary = File(applicationInfo.nativeLibraryDir, GO_BINARY_NAME)
            Log.i(TAG, "Looking for binary at: ${binary.absolutePath}")

            if (!binary.exists()) {
                Log.e(TAG, "Go binary not found at ${binary.absolutePath}")
                stopSelf()
                return
            }

            if (!binary.canExecute()) {
                Log.e(TAG, "Go binary not executable at ${binary.absolutePath}")
                stopSelf()
                return
            }

            val dataDir = File(filesDir, "pocketcluster")
            dataDir.mkdirs()

            // Acquire wake lock to keep CPU running
            val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
            wakeLock = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "PocketCluster::Agent").apply {
                acquire()
            }

            // Acquire multicast lock for mDNS
            val wm = applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
            multicastLock = wm.createMulticastLock("PocketCluster::mDNS").apply {
                setReferenceCounted(true)
                acquire()
            }

            val deviceName = Build.MODEL ?: "Android Device"
            val wifiIP = getWifiIP()
            val pb = ProcessBuilder(
                binary.absolutePath,
                "-data", dataDir.absolutePath,
                "-port", DEFAULT_PORT.toString(),
                "-name", deviceName,
                "-iface", "wlan0",
                "-advertise-ip", wifiIP ?: "",
                "-local-ip", wifiIP ?: "",
            )
            pb.redirectErrorStream(true)
            pb.environment()["HOME"] = filesDir.absolutePath

            process = pb.start()

            // Read stdout in background thread for logging and nodeId extraction
            logThread = Thread {
                try {
                    val reader = BufferedReader(InputStreamReader(process!!.inputStream))
                    var line: String?
                    while (reader.readLine().also { line = it } != null) {
                        Log.i(TAG, "[agent] $line")
                        // Parse nodeId from log: "listening on :7788 (node_id=xxx)"
                        if (line?.contains("node_id=") == true) {
                            val match = Regex("node_id=([a-f0-9-]+)").find(line!!)
                            if (match != null) {
                                nodeId = match.groupValues[1]
                                Log.i(TAG, "Parsed nodeId: $nodeId")
                            }
                        }
                    }
                } catch (e: Exception) {
                    Log.d(TAG, "Log reader stopped: ${e.message}")
                }
            }.apply { isDaemon = true; start() }

            isRunning = true
            updateNotification("Running on port $DEFAULT_PORT")
            Log.i(TAG, "Go agent started (pid=${process?.hashCode()})")

            // Monitor process exit
            Thread {
                try {
                    val exitCode = process?.waitFor()
                    Log.w(TAG, "Go agent exited with code $exitCode")
                    isRunning = false
                    updateNotification("Agent stopped (exit=$exitCode)")
                } catch (e: InterruptedException) {
                    Log.d(TAG, "Process monitor interrupted")
                }
            }.apply { isDaemon = true; start() }

        } catch (e: Exception) {
            Log.e(TAG, "Failed to start agent", e)
            stopSelf()
        }
    }

    private fun stopAgent() {
        isRunning = false
        nodeId = null

        process?.let {
            if (it.isAlive) {
                it.destroy()
                // Give it a moment, then force kill
                Thread.sleep(500)
                if (it.isAlive) {
                    it.destroyForcibly()
                }
            }
        }
        process = null

        logThread?.interrupt()
        logThread = null

        multicastLock?.let { if (it.isHeld) it.release() }
        multicastLock = null

        wakeLock?.let { if (it.isHeld) it.release() }
        wakeLock = null

        Log.i(TAG, "Agent stopped")
    }

    private fun buildNotification(text: String): Notification {
        val pendingIntent = PendingIntent.getActivity(
            this, 0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE,
        )
        return Notification.Builder(this, PocketClusterApp.CHANNEL_ID)
            .setContentTitle("PocketCluster")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_dialog_info)
            .setContentIntent(pendingIntent)
            .setOngoing(true)
            .build()
    }

    private fun updateNotification(text: String) {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as android.app.NotificationManager
        nm.notify(NOTIFICATION_ID, buildNotification(text))
    }

    private fun getWifiIP(): String? {
        try {
            val wm = applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
            val ip = wm.connectionInfo.ipAddress
            if (ip != 0) {
                return String.format("%d.%d.%d.%d",
                    ip and 0xff,
                    ip shr 8 and 0xff,
                    ip shr 16 and 0xff,
                    ip shr 24 and 0xff)
            }
        } catch (e: Exception) {
            Log.w(TAG, "Failed to get WiFi IP: ${e.message}")
        }

        try {
            val interfaces = NetworkInterface.getNetworkInterfaces()
            while (interfaces.hasMoreElements()) {
                val intf = interfaces.nextElement()
                if (intf.isLoopback || !intf.isUp) continue
                val addresses = intf.inetAddresses
                while (addresses.hasMoreElements()) {
                    val addr = addresses.nextElement()
                    if (addr is Inet4Address && !addr.isLoopbackAddress) {
                        return addr.hostAddress
                    }
                }
            }
        } catch (e: Exception) {
            Log.w(TAG, "Failed to enumerate network interfaces: ${e.message}")
        }
        return null
    }
}
