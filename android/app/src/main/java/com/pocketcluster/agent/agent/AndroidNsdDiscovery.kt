package com.pocketcluster.agent.agent

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import java.net.Inet4Address
import java.nio.charset.StandardCharsets
import java.util.ArrayDeque

internal class AndroidNsdDiscovery(
    context: Context,
    private val localNodeId: String,
    private val nodeName: String,
    private val port: Int,
    private val log: (String) -> Unit,
    private val onUpsert: (nodeId: String, name: String, platform: String, ip: String, port: Int) -> Unit,
    private val onRemove: (nodeId: String) -> Unit,
) {
    companion object {
        private const val SERVICE_TYPE = "_pocketcluster._tcp."
    }

    private val nsdManager = context.getSystemService(Context.NSD_SERVICE) as NsdManager
    private val resolvedNodeIds = mutableMapOf<String, String>()
    private val resolveQueue = ArrayDeque<NsdServiceInfo>()
    private val pendingResolveKeys = mutableSetOf<String>()

    private var started = false
    private var discoveryRequested = false
    private var registrationRequested = false
    private var resolveActive = false

    private val registrationListener = object : NsdManager.RegistrationListener {
        override fun onServiceRegistered(serviceInfo: NsdServiceInfo) {
            if (!started) {
                runCatching { nsdManager.unregisterService(this) }
                return
            }
            log("Registered ${serviceInfo.serviceName} on port $port")
        }

        override fun onRegistrationFailed(serviceInfo: NsdServiceInfo, errorCode: Int) {
            registrationRequested = false
            log("Registration failed (code=$errorCode)")
        }

        override fun onServiceUnregistered(serviceInfo: NsdServiceInfo) {
            registrationRequested = false
        }

        override fun onUnregistrationFailed(serviceInfo: NsdServiceInfo, errorCode: Int) {
            log("Unregistration failed (code=$errorCode)")
        }
    }

    private val discoveryListener = object : NsdManager.DiscoveryListener {
        override fun onDiscoveryStarted(serviceType: String) {
            if (!started) {
                runCatching { nsdManager.stopServiceDiscovery(this) }
                return
            }
            log("Discovery started")
        }

        override fun onStartDiscoveryFailed(serviceType: String, errorCode: Int) {
            discoveryRequested = false
            log("Discovery failed to start (code=$errorCode)")
        }

        override fun onDiscoveryStopped(serviceType: String) {
            discoveryRequested = false
        }

        override fun onStopDiscoveryFailed(serviceType: String, errorCode: Int) {
            log("Discovery failed to stop (code=$errorCode)")
        }

        override fun onServiceFound(serviceInfo: NsdServiceInfo) {
            if (!started) return
            enqueueResolve(serviceInfo)
        }

        override fun onServiceLost(serviceInfo: NsdServiceInfo) {
            removeResolvedService(serviceKey(serviceInfo))
        }
    }

    fun start() {
        if (started) return
        started = true

        val serviceInfo = NsdServiceInfo().apply {
            serviceName = localNodeId
            serviceType = SERVICE_TYPE
            setPort(this@AndroidNsdDiscovery.port)
            setAttribute("id", localNodeId)
            setAttribute("name", nodeName)
            setAttribute("platform", "android")
        }

        try {
            registrationRequested = true
            nsdManager.registerService(serviceInfo, NsdManager.PROTOCOL_DNS_SD, registrationListener)
        } catch (e: Exception) {
            registrationRequested = false
            log("Registration request failed: ${e.message}")
        }
        try {
            discoveryRequested = true
            nsdManager.discoverServices(SERVICE_TYPE, NsdManager.PROTOCOL_DNS_SD, discoveryListener)
        } catch (e: Exception) {
            discoveryRequested = false
            log("Discovery request failed: ${e.message}")
        }
    }

    fun stop() {
        if (!started) return
        started = false

        resolvedNodeIds.clear()
        resolveQueue.clear()
        pendingResolveKeys.clear()
        resolveActive = false

        if (discoveryRequested) {
            runCatching { nsdManager.stopServiceDiscovery(discoveryListener) }
        }
        if (registrationRequested) {
            runCatching { nsdManager.unregisterService(registrationListener) }
        }
    }

    @Suppress("DEPRECATION")
    private fun enqueueResolve(serviceInfo: NsdServiceInfo) {
        val key = serviceKey(serviceInfo)
        if (!started || !pendingResolveKeys.add(key)) return
        resolveQueue.addLast(serviceInfo)
        resolveNext()
    }

    @Suppress("DEPRECATION")
    private fun resolveNext() {
        if (!started || resolveActive) return
        val serviceInfo = resolveQueue.pollFirst() ?: return
        val key = serviceKey(serviceInfo)
        resolveActive = true
        try {
            nsdManager.resolveService(serviceInfo, object : NsdManager.ResolveListener {
                override fun onResolveFailed(failedInfo: NsdServiceInfo, errorCode: Int) {
                    pendingResolveKeys.remove(key)
                    resolveActive = false
                    log("Service resolution failed for ${failedInfo.serviceName} (code=$errorCode)")
                    resolveNext()
                }

                override fun onServiceResolved(resolvedInfo: NsdServiceInfo) {
                    pendingResolveKeys.remove(key)
                    resolveActive = false
                    handleResolved(key, resolvedInfo)
                    resolveNext()
                }
            })
        } catch (e: Exception) {
            pendingResolveKeys.remove(key)
            resolveActive = false
            log("Service resolution request failed for ${serviceInfo.serviceName}: ${e.message}")
            resolveNext()
        }
    }

    @Suppress("DEPRECATION")
    private fun handleResolved(key: String, serviceInfo: NsdServiceInfo) {
        if (!started) return

        val nodeId = attribute(serviceInfo, "id") ?: serviceInfo.serviceName
        if (nodeId.isBlank() || nodeId == localNodeId) return

        val host = serviceInfo.host
        val ip = host.takeIf { it is Inet4Address && !it.isLoopbackAddress }?.hostAddress
        if (ip.isNullOrBlank() || serviceInfo.port !in 1..65535) {
            log("Resolved ${serviceInfo.serviceName} without a usable IPv4 address")
            return
        }

        val name = attribute(serviceInfo, "name") ?: serviceInfo.serviceName
        val platform = attribute(serviceInfo, "platform") ?: "unknown"
        resolvedNodeIds[key] = nodeId
        onUpsert(nodeId, name, platform, ip, serviceInfo.port)
        log("Discovered $name at $ip:${serviceInfo.port}")
    }

    private fun removeResolvedService(key: String) {
        resolvedNodeIds.remove(key)?.let(onRemove)
    }

    private fun attribute(serviceInfo: NsdServiceInfo, key: String): String? {
        return serviceInfo.attributes[key]
            ?.toString(StandardCharsets.UTF_8)
            ?.takeIf { it.isNotBlank() }
    }

    private fun serviceKey(serviceInfo: NsdServiceInfo): String {
        return "${serviceInfo.serviceName}|${serviceInfo.serviceType}"
    }
}
