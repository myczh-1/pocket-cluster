package com.pocketcluster.agent

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.view.View
import android.widget.Button
import android.widget.TextView
import android.widget.Toast
import com.pocketcluster.agent.agent.AgentService

class MainActivity : Activity() {

    private lateinit var tvStatus: TextView
    private lateinit var tvNodeInfo: TextView
    private lateinit var btnToggle: Button
    private lateinit var btnWebUI: Button

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        tvStatus = findViewById(R.id.tvStatus)
        tvNodeInfo = findViewById(R.id.tvNodeInfo)
        btnToggle = findViewById(R.id.btnToggle)
        btnWebUI = findViewById(R.id.btnWebUI)

        btnToggle.setOnClickListener { onToggleClicked() }
        btnWebUI.setOnClickListener {
            startActivity(Intent(this, WebUIActivity::class.java))
        }

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
        grantResults: IntArray,
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

    private fun updateUI() {
        if (AgentService.isRunning) {
            tvStatus.text = "Running"
            tvStatus.setTextColor(0xFF4CAF50.toInt())
            btnToggle.text = "Stop Agent"
            tvNodeInfo.text = "Go agent running on port 7788"
            tvNodeInfo.visibility = View.VISIBLE
            btnWebUI.visibility = View.VISIBLE
        } else {
            tvStatus.text = "Stopped"
            tvStatus.setTextColor(0xFF888888.toInt())
            btnToggle.text = "Start Agent"
            tvNodeInfo.visibility = View.GONE
            btnWebUI.visibility = View.GONE
        }
    }

    companion object {
        private const val REQ_NOTIFICATION = 1001
    }
}
