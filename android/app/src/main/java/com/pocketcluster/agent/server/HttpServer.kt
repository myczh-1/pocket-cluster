package com.pocketcluster.agent.server

import android.util.Log
import com.pocketcluster.agent.config.NodeConfig
import com.pocketcluster.agent.discovery.MdnsDiscovery
import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer as JdkHttpServer
import java.io.OutputStream
import java.net.InetSocketAddress
import java.util.concurrent.Executors

class HttpServer(
    private val config: NodeConfig,
    private val discovery: MdnsDiscovery,
) {
    companion object {
        private const val TAG = "HttpServer"
    }

    private val startedAt = System.currentTimeMillis()
    private var server: JdkHttpServer? = null

    val port: Int get() = config.httpPort

    fun start() {
        val s = JdkHttpServer.create(InetSocketAddress("0.0.0.0", config.httpPort), 0)
        s.executor = Executors.newFixedThreadPool(4)

        s.createContext("/api/health") { handleHealth(it) }
        s.createContext("/api/node/info") { handleNodeInfo(it) }
        s.createContext("/api/nodes") { handleNotImplemented(it) }
        s.createContext("/api/nodes/discovered") { handleDiscoveredNodes(it) }
        s.createContext("/api/files") { handleNotImplemented(it) }
        s.createContext("/api/invites") { handleNotImplemented(it) }
        s.createContext("/api/join") { handleNotImplemented(it) }
        s.createContext("/api/events") { handleNotImplemented(it) }
        s.createContext("/api/chunks") { handleNotImplemented(it) }
        s.createContext("/api/snapshot") { handleNotImplemented(it) }

        s.start()
        server = s
        Log.i(TAG, "HTTP server listening on 0.0.0.0:${config.httpPort}")
    }

    fun stop() {
        server?.stop(0)
        server = null
    }

    private fun handleHealth(exchange: HttpExchange) {
        val uptimeSec = (System.currentTimeMillis() - startedAt) / 1000
        val body = """{"ok":true,"data":{"node_id":"${config.nodeId}","status":"online","uptime_seconds":$uptimeSec}}"""
        sendJson(exchange, 200, body)
    }

    private fun handleNodeInfo(exchange: HttpExchange) {
        val body = buildString {
            append("""{"ok":true,"data":{""")
            append(""""node_id":"${config.nodeId}",""")
            append(""""name":"${config.name}",""")
            append(""""platform":"${config.platform}",""")
            append(""""cluster_id":"${config.clusterId}",""")
            append(""""discovery_mode":"${config.discoveryMode}",""")
            append(""""status":"online"""")
            append("}}")
        }
        sendJson(exchange, 200, body)
    }

    private fun handleDiscoveredNodes(exchange: HttpExchange) {
        val nodes = discovery.discoveredNodes
        val nodesJson = nodes.joinToString(",") { n ->
            """{"node_id":"${n.nodeId}","name":"${n.name}","platform":"${n.platform}","address":"${n.address}:${n.port}"}"""
        }
        val body = """{"ok":true,"data":[$nodesJson]}"""
        sendJson(exchange, 200, body)
    }

    private fun handleNotImplemented(exchange: HttpExchange) {
        val body = """{"ok":false,"error":{"code":"NOT_IMPLEMENTED","message":"Endpoint not yet implemented on Android agent"}}"""
        sendJson(exchange, 501, body)
    }

    private fun sendJson(exchange: HttpExchange, status: Int, body: String) {
        val bytes = body.toByteArray(Charsets.UTF_8)
        exchange.responseHeaders.set("Content-Type", "application/json")
        exchange.sendResponseHeaders(status, bytes.size.toLong())
        val os: OutputStream = exchange.responseBody
        os.write(bytes)
        os.close()
    }
}
