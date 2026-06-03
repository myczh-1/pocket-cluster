package com.pocketcluster.agent

import android.Manifest
import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.wifi.WifiManager
import android.os.Build
import android.os.Bundle
import android.view.View
import android.widget.Button
import android.widget.TextView
import android.widget.Toast
import com.pocketcluster.agent.agent.AgentService
import java.net.Inet4Address
import java.net.NetworkInterface

class MainActivity : Activity() {

    private lateinit var tvStatus: TextView
    private lateinit var addressCard: View
    private lateinit var tvAddress: TextView
    private lateinit var tvNodeId: TextView
    private lateinit var tvNodeInfo: TextView
    private lateinit var btnToggle: Button
    private lateinit var btnWebUI: Button
    private lateinit var btnCopy: Button

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

        btnToggle.setOnClickListener { onToggleClicked() }
        btnWebUI.setOnClickListener {
            startActivity(Intent(this, WebUIActivity::class.java))
        }
        btnCopy.setOnClickListener { copyAddress() }

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

    private fun getWifiIpAddress(): String? {
        try {
            val wifiManager = applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
            val wifiInfo = wifiManager.connectionInfo
            val ip = wifiInfo.ipAddress
            if (ip != 0) {
                return String.format(
                    "%d.%d.%d.%d",
                    ip and 0xff,
                    ip shr 8 and 0xff,
                    ip shr 16 and 0xff,
                    ip shr 24 and 0xff
                )
            }
        } catch (e: Exception) {
            // Fall through to NetworkInterface
        }

        // Fallback to NetworkInterface
        try {
            for (intf in NetworkInterface.getNetworkInterfaces()) {
                if (intf.isLoopback || !intf.isUp) continue
                for (addr in intf.inetAddresses) {
                    if (addr is Inet4Address && !addr.isLoopbackAddress) {
                        return addr.hostAddress
                    }
                }
            }
        } catch (e: Exception) {
            // Ignore
        }
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
    }

    companion object {
        private const val REQ_NOTIFICATION = 1001
    }
}
