package com.pocketcluster.agent.discovery

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.util.Log
import java.util.concurrent.ConcurrentHashMap

data class DiscoveredNode(
    val nodeId: String,
    val name: String,
    val platform: String,
    val address: String,
    val port: Int,
)

class MdnsDiscovery(
    context: Context,
    private val selfNodeId: String,
    private val selfName: String,
    private val selfPort: Int,
) {
    companion object {
        private const val TAG = "MdnsDiscovery"
        private const val SERVICE_TYPE = "_pocketcluster._tcp."
    }

    private val nsdManager = context.getSystemService(Context.NSD_SERVICE) as NsdManager
    private val nodes = ConcurrentHashMap<String, DiscoveredNode>()
    private var registrationListener: NsdManager.RegistrationListener? = null
    private var discoveryListener: NsdManager.DiscoveryListener? = null

    val discoveredNodes: List<DiscoveredNode>
        get() = nodes.values.toList()

    fun start() {
        register()
        discover()
    }

    fun stop() {
        try {
            registrationListener?.let { nsdManager.unregisterService(it) }
        } catch (e: Exception) {
            Log.w(TAG, "Unregister error: ${e.message}")
        }
        try {
            discoveryListener?.let { nsdManager.stopServiceDiscovery(it) }
        } catch (e: Exception) {
            Log.w(TAG, "Stop discovery error: ${e.message}")
        }
        registrationListener = null
        discoveryListener = null
    }

    private fun register() {
        val info = NsdServiceInfo().apply {
            serviceName = selfNodeId
            serviceType = SERVICE_TYPE
            setPort(selfPort)
            setAttribute("id", selfNodeId)
            setAttribute("name", selfName)
            setAttribute("platform", "android")
        }

        val listener = object : NsdManager.RegistrationListener {
            override fun onServiceRegistered(s: NsdServiceInfo) {
                Log.i(TAG, "Registered: ${s.serviceName}")
            }
            override fun onRegistrationFailed(s: NsdServiceInfo, errorCode: Int) {
                Log.e(TAG, "Registration failed: $errorCode")
            }
            override fun onServiceUnregistered(s: NsdServiceInfo) {}
            override fun onUnregistrationFailed(s: NsdServiceInfo, errorCode: Int) {}
        }
        registrationListener = listener
        nsdManager.registerService(info, NsdManager.PROTOCOL_DNS_SD, listener)
    }

    private fun discover() {
        val listener = object : NsdManager.DiscoveryListener {
            override fun onDiscoveryStarted(regType: String) {
                Log.d(TAG, "Discovery started")
            }
            override fun onServiceFound(service: NsdServiceInfo) {
                if (service.serviceType != SERVICE_TYPE) return
                nsdManager.resolveService(service, object : NsdManager.ResolveListener {
                    override fun onResolveFailed(s: NsdServiceInfo, errorCode: Int) {
                        Log.w(TAG, "Resolve failed: $errorCode")
                    }
                    override fun onServiceResolved(s: NsdServiceInfo) {
                        handleResolved(s)
                    }
                })
            }
            override fun onServiceLost(service: NsdServiceInfo) {
                Log.d(TAG, "Service lost: ${service.serviceName}")
                nodes.remove(service.serviceName)
            }
            override fun onDiscoveryStopped(serviceType: String) {}
            override fun onStartDiscoveryFailed(serviceType: String, errorCode: Int) {
                Log.e(TAG, "Discovery start failed: $errorCode")
            }
            override fun onStopDiscoveryFailed(serviceType: String, errorCode: Int) {}
        }
        discoveryListener = listener
        nsdManager.discoverServices(SERVICE_TYPE, NsdManager.PROTOCOL_DNS_SD, listener)
    }

    private fun handleResolved(s: NsdServiceInfo) {
        val host = s.host?.hostAddress ?: return
        val nodeId = s.attributes["id"]?.let { String(it) } ?: s.serviceName
        if (nodeId == selfNodeId) return
        val name = s.attributes["name"]?.let { String(it) } ?: ""
        val platform = s.attributes["platform"]?.let { String(it) } ?: ""
        val node = DiscoveredNode(nodeId, name, platform, host, s.port)
        nodes[nodeId] = node
        Log.i(TAG, "Discovered: $name ($nodeId) at $host:${s.port}")
    }
}
