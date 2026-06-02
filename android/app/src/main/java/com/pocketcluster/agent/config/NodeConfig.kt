package com.pocketcluster.agent.config

import android.content.Context
import android.util.Log
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.io.File
import java.util.UUID

@Serializable
data class NodeConfig(
    val nodeId: String,
    val name: String,
    val platform: String = "android",
    val clusterId: String = "",
    val publicKey: String,
    val secretKey: String,
    val httpPort: Int = 7788,
    val discoveryMode: String = "auto",
) {
    fun save(dir: File) {
        File(dir, "config.json").writeText(json.encodeToString(serializer(), this))
    }

    companion object {
        private val json = Json { prettyPrint = true; ignoreUnknownKeys = true }

        fun load(dir: File): NodeConfig? {
            val f = File(dir, "config.json")
            if (!f.exists()) return null
            return json.decodeFromString<NodeConfig>(f.readText())
        }

        fun create(context: Context, dir: File): NodeConfig {
            dir.mkdirs()
            val (pub, priv) = Ed25519.generateKeypair()
            val cfg = NodeConfig(
                nodeId = UUID.randomUUID().toString(),
                name = "${android.os.Build.MANUFACTURER} ${android.os.Build.MODEL}",
                publicKey = pub,
                secretKey = priv,
            )
            cfg.save(dir)
            return cfg
        }
    }
}

private object Ed25519 {
    fun generateKeypair(): Pair<String, String> {
        val kpg = java.security.KeyPairGenerator.getInstance("Ed25519")
        try {
            // Android 13+ (API 33): create EdDSAParameterSpec(NamedParameterSpec.ED25519) via reflection
            val namedSpecClass = Class.forName("java.security.spec.NamedParameterSpec")
            val ed25519Field = namedSpecClass.getField("ED25519")
            val ed25519Spec = ed25519Field.get(null)
            val eddsaSpecClass = Class.forName("java.security.spec.EdDSAParameterSpec")
            val ctor = eddsaSpecClass.getConstructor(namedSpecClass)
            val spec = ctor.newInstance(ed25519Spec) as java.security.spec.AlgorithmParameterSpec
            kpg.initialize(spec)
        } catch (e: Exception) {
            // Fallback: use EC P-256 if Ed25519 not available
            Log.w("Ed25519", "Ed25519 init failed, falling back to EC P-256", e)
            val ecKpg = java.security.KeyPairGenerator.getInstance("EC")
            ecKpg.initialize(java.security.spec.ECGenParameterSpec("secp256r1"))
            val kp = ecKpg.generateKeyPair()
            val pub = android.util.Base64.encodeToString(kp.public.encoded, android.util.Base64.NO_WRAP)
            val priv = android.util.Base64.encodeToString(kp.private.encoded, android.util.Base64.NO_WRAP)
            return pub to priv
        }
        val kp = kpg.generateKeyPair()
        val pub = android.util.Base64.encodeToString(kp.public.encoded, android.util.Base64.NO_WRAP)
        val priv = android.util.Base64.encodeToString(kp.private.encoded, android.util.Base64.NO_WRAP)
        return pub to priv
    }
}
