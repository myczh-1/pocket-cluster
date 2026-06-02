package com.pocketcluster.agent.agent

import android.app.Notification
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.IBinder
import android.os.PowerManager
import android.util.Log
import com.pocketcluster.agent.MainActivity
import com.pocketcluster.agent.PocketClusterApp
import com.pocketcluster.agent.R
import com.pocketcluster.agent.config.NodeConfig
import com.pocketcluster.agent.discovery.MdnsDiscovery
import com.pocketcluster.agent.server.HttpServer
import java.io.File

class AgentService : Service() {

    companion object {
        private const val TAG = "AgentService"
        private const val NOTIFICATION_ID = 1

        var isRunning: Boolean = false
            private set

        var currentNodeConfig: NodeConfig? = null
            private set

        fun start(context: Context) {
            val intent = Intent(context, AgentService::class.java)
            context.startForegroundService(intent)
        }

        fun stop(context: Context) {
            val intent = Intent(context, AgentService::class.java)
            context.stopService(intent)
        }
    }

    private var httpServer: HttpServer? = null
    private var discovery: MdnsDiscovery? = null
    private var wakeLock: PowerManager.WakeLock? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        Log.i(TAG, "Agent service created")
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForeground(NOTIFICATION_ID, buildNotification("Starting..."))
        startAgent()
        return START_STICKY
    }

    override fun onDestroy() {
        stopAgent()
        super.onDestroy()
    }

    private fun startAgent() {
        try {
            val dataDir = File(filesDir, "pocketcluster")
            val config = NodeConfig.load(dataDir) ?: NodeConfig.create(this, dataDir)
            currentNodeConfig = config

            Log.i(TAG, "Node ID: ${config.nodeId}")
            Log.i(TAG, "Name: ${config.name}")
            Log.i(TAG, "Port: ${config.httpPort}")

            // Start mDNS discovery
            discovery = MdnsDiscovery(
                context = this,
                selfNodeId = config.nodeId,
                selfName = config.name,
                selfPort = config.httpPort,
            ).also { it.start() }

            // Start HTTP server
            httpServer = HttpServer(config, discovery!!).also { it.start() }

            // Acquire partial wake lock to keep CPU running
            val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
            wakeLock = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "PocketCluster::Agent").apply {
                acquire()
            }

            isRunning = true
            updateNotification("Running on port ${config.httpPort}")
            Log.i(TAG, "Agent started successfully")
        } catch (e: Exception) {
            Log.e(TAG, "Failed to start agent", e)
            stopSelf()
        }
    }

    private fun stopAgent() {
        wakeLock?.let { if (it.isHeld) it.release() }
        discovery?.stop()
        httpServer?.stop()
        isRunning = false
        currentNodeConfig = null
        Log.i(TAG, "Agent stopped")
    }

    private fun buildNotification(text: String): Notification {
        val pendingIntent = PendingIntent.getActivity(
            this, 0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        return Notification.Builder(this, PocketClusterApp.CHANNEL_ID)
            .setContentTitle("PocketCluster")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_menu_info_details)
            .setContentIntent(pendingIntent)
            .setOngoing(true)
            .build()
    }

    private fun updateNotification(text: String) {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as android.app.NotificationManager
        nm.notify(NOTIFICATION_ID, buildNotification(text))
    }
}
