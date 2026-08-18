package com.pocketcluster.agent.agent

import android.app.Notification
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.net.wifi.WifiManager
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import android.util.Log
import com.pocketcluster.agent.MainActivity
import com.pocketcluster.agent.PocketClusterApp
import com.pocketcluster.agent.R
import java.io.BufferedReader
import java.io.BufferedWriter
import java.io.File
import java.io.InputStreamReader
import java.io.OutputStreamWriter
import java.net.Inet4Address
import java.net.NetworkInterface
import java.nio.charset.StandardCharsets
import java.util.Collections
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import org.json.JSONObject

class AgentService : Service() {

    companion object {
        private const val TAG = "AgentService"
        private const val NOTIFICATION_ID = 1
        private const val GO_BINARY_NAME = "libpocketcluster.so"
        private const val DEFAULT_PORT = 7788
        private const val MAX_LOG_LINES = 500
        private const val MAX_RESTART_ATTEMPTS = 3
        private const val RESTART_DELAY_MS = 3000L
        private const val EXIT_SIGILL = 132

        var isRunning: Boolean = false
            private set
        var nodeId: String? = null
            private set
        val logLines: MutableList<String> = Collections.synchronizedList(mutableListOf<String>())
    }
    private var process: Process? = null
    private var wakeLock: PowerManager.WakeLock? = null
    private var multicastLock: WifiManager.MulticastLock? = null
    private var logThread: Thread? = null
    private var monitorThread: Thread? = null
    private var discoveryWriter: BufferedWriter? = null
    private var nsdDiscovery: AndroidNsdDiscovery? = null
    private val discoveryWriterLock = Any()
    private val mainHandler = Handler(Looper.getMainLooper())
    private val restartCount = AtomicInteger(0)
    private val shouldRun = AtomicBoolean(false)
    private var isStopping = false

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        Log.i(TAG, "Agent service created")
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            "STOP" -> {
                isStopping = true
                stopAgent()
                stopSelf()
                return START_NOT_STICKY
            }
        }
        startForeground(NOTIFICATION_ID, buildNotification("Starting agent..."))
        if (!isRunning) {
            shouldRun.set(true)
            isStopping = false
            restartCount.set(0)
            startAgent()
        }
        return START_STICKY
    }

    override fun onDestroy() {
        isStopping = true
        shouldRun.set(false)
        stopAgent()
        super.onDestroy()
    }

    private fun startAgent() {
        try {
            val binary = File(applicationInfo.nativeLibraryDir, GO_BINARY_NAME)
            addLog("Looking for binary at: ${binary.absolutePath}")

            if (!binary.exists()) {
                addLog("ERROR: Go binary not found at ${binary.absolutePath}")
                updateNotification("Error: binary not found")
                stopSelf()
                return
            }

            if (!binary.canExecute()) {
                addLog("ERROR: Go binary not executable")
                updateNotification("Error: binary not executable")
                stopSelf()
                return
            }

            val dataDir = File(filesDir, "pocketcluster")
            dataDir.mkdirs()

            acquireWakeLocks()

            val deviceName = Build.MODEL ?: "Android Device"
            val wifiIP = getWifiIP()
            val pb = ProcessBuilder(
                binary.absolutePath,
                "-data", dataDir.absolutePath,
                "-port", DEFAULT_PORT.toString(),
                "-name", deviceName,
                "-local-ip", wifiIP ?: "",
                "-external-discovery",
            )
            pb.redirectErrorStream(true)
            pb.environment()["HOME"] = filesDir.absolutePath

            val startedProcess = pb.start()
            val startedDiscoveryWriter = BufferedWriter(
                OutputStreamWriter(startedProcess.outputStream, StandardCharsets.UTF_8),
            )
            process = startedProcess
            synchronized(discoveryWriterLock) {
                discoveryWriter = startedDiscoveryWriter
            }
            isRunning = true
            addLog("Agent started (port=$DEFAULT_PORT, ip=${wifiIP ?: "unknown"})")
            updateNotification("Running on port $DEFAULT_PORT")

            // Read stdout for logging and nodeId extraction
            logThread = Thread {
                try {
                    val reader = BufferedReader(InputStreamReader(startedProcess.inputStream))
                    var line: String?
                    while (reader.readLine().also { line = it } != null) {
                        addLog("[agent] $line")
                        if (line?.contains("node_id=") == true) {
                            val match = Regex("node_id=([a-f0-9-]+)").find(line!!)
                            if (match != null) {
                                val discoveredNodeId = match.groupValues[1]
                                nodeId = discoveredNodeId
                                addLog("Node ID: $discoveredNodeId")
                                mainHandler.post {
                                    startNsdDiscovery(discoveredNodeId, deviceName)
                                }
                            }
                        }
                    }
                } catch (e: Exception) {
                    Log.d(TAG, "Log reader stopped: ${e.message}")
                }
            }.apply { isDaemon = true; start() }

            // Monitor process exit and auto-restart
            monitorThread = Thread {
                try {
                    val exitCode = startedProcess.waitFor()
                    isRunning = false
                    addLog("Agent exited with code $exitCode")
                    synchronized(discoveryWriterLock) {
                        if (discoveryWriter === startedDiscoveryWriter) {
                            discoveryWriter = null
                        }
                    }
                    runCatching { startedDiscoveryWriter.close() }
                    mainHandler.post { stopNsdDiscovery() }

                    val fatalMessage = fatalExitMessage(exitCode)
                    if (fatalMessage != null) {
                        addLog(fatalMessage)
                        shouldRun.set(false)
                        process = null
                        releaseWakeLocks()
                        updateNotification("Unsupported CPU/emulator ABI")
                        return@Thread
                    }

                    updateNotification("Agent stopped (exit=$exitCode)")

                    // Auto-restart if not intentionally stopped
                    if (shouldRun.get() && !isStopping) {
                        val attempt = restartCount.incrementAndGet()
                        if (attempt <= MAX_RESTART_ATTEMPTS) {
                            addLog("Restarting (attempt $attempt/$MAX_RESTART_ATTEMPTS)...")
                            updateNotification("Restarting (attempt $attempt)...")
                            Thread.sleep(RESTART_DELAY_MS)
                            if (shouldRun.get() && !isStopping) {
                                startAgent()
                            }
                        } else {
                            addLog("Max restart attempts reached. Stopping.")
                            updateNotification("Crashed — tap to restart")
                        }
                    }
                } catch (e: InterruptedException) {
                    Log.d(TAG, "Process monitor interrupted")
                }
            }.apply { isDaemon = true; start() }

        } catch (e: Exception) {
            Log.e(TAG, "Failed to start agent", e)
            addLog("ERROR: Failed to start: ${e.message}")
            updateNotification("Error: ${e.message}")
            stopSelf()
        }
    }

    private fun stopAgent() {
        shouldRun.set(false)
        isRunning = false
        nodeId = null
        stopNsdDiscovery()

        synchronized(discoveryWriterLock) {
            runCatching { discoveryWriter?.close() }
            discoveryWriter = null
        }

        process?.let {
            if (it.isAlive) {
                // Graceful shutdown first
                it.destroy()
                if (!it.waitFor(2000, java.util.concurrent.TimeUnit.MILLISECONDS)) {
                    it.destroyForcibly()
                }
            }
        }
        process = null

        logThread?.interrupt()
        logThread = null
        monitorThread?.interrupt()
        monitorThread = null

        releaseWakeLocks()
        addLog("Agent stopped")
    }

    private fun acquireWakeLocks() {
        if (wakeLock?.isHeld != true) {
            val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
            wakeLock = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "PocketCluster::Agent").apply {
                acquire()
            }
        }
        if (multicastLock?.isHeld != true) {
            val wm = applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
            multicastLock = wm.createMulticastLock("PocketCluster::mDNS").apply {
                setReferenceCounted(true)
                acquire()
            }
        }
    }

    private fun startNsdDiscovery(discoveredNodeId: String, deviceName: String) {
        if (!shouldRun.get() || isStopping || nsdDiscovery != null) return
        nsdDiscovery = AndroidNsdDiscovery(
            context = applicationContext,
            localNodeId = discoveredNodeId,
            nodeName = deviceName,
            port = DEFAULT_PORT,
            log = { message -> addLog("[mDNS] $message") },
            onUpsert = { id, name, platform, ip, port ->
                writeDiscoveryEvent("upsert", id, name, platform, ip, port)
            },
            onRemove = { id -> writeDiscoveryEvent("remove", id) },
        ).also { it.start() }
    }

    private fun stopNsdDiscovery() {
        nsdDiscovery?.stop()
        nsdDiscovery = null
    }

    private fun writeDiscoveryEvent(
        op: String,
        id: String,
        name: String? = null,
        platform: String? = null,
        ip: String? = null,
        port: Int? = null,
    ) {
        val event = JSONObject()
            .put("op", op)
            .put("node_id", id)
        if (name != null) event.put("name", name)
        if (platform != null) event.put("platform", platform)
        if (ip != null) event.put("ip", ip)
        if (port != null) event.put("port", port)

        synchronized(discoveryWriterLock) {
            val writer = discoveryWriter ?: return
            try {
                writer.write(event.toString())
                writer.newLine()
                writer.flush()
            } catch (e: Exception) {
                addLog("[mDNS] Failed to forward discovery event: ${e.message}")
            }
        }
    }

    private fun releaseWakeLocks() {
        multicastLock?.let { if (it.isHeld) it.release() }
        multicastLock = null
        wakeLock?.let { if (it.isHeld) it.release() }
        wakeLock = null
    }

    private fun fatalExitMessage(exitCode: Int?): String? {
        if (exitCode != EXIT_SIGILL) return null

        val abiList = Build.SUPPORTED_ABIS.joinToString(", ")
        return "ERROR: Native agent crashed with SIGILL (illegal instruction). Device ABIs: $abiList. Please collect: adb logcat -b crash -d"
    }

    private fun addLog(line: String) {
        Log.i(TAG, line)
        synchronized(logLines) {
            logLines.add(line)
            while (logLines.size > MAX_LOG_LINES) logLines.removeAt(0)
        }
    }

    private fun buildNotification(text: String): Notification {
        val pendingIntent = PendingIntent.getActivity(
            this, 0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE,
        )
        val stopIntent = PendingIntent.getService(
            this, 1,
            Intent(this, AgentService::class.java).setAction("STOP"),
            PendingIntent.FLAG_IMMUTABLE,
        )
        return Notification.Builder(this, PocketClusterApp.CHANNEL_ID)
            .setContentTitle("PocketCluster")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_dialog_info)
            .setContentIntent(pendingIntent)
            .addAction(Notification.Action.Builder(
                null, "Stop", stopIntent
            ).build())
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
                    ip and 0xff, ip shr 8 and 0xff, ip shr 16 and 0xff, ip shr 24 and 0xff)
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
