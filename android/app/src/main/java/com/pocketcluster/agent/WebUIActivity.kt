package com.pocketcluster.agent

import android.annotation.SuppressLint
import android.os.Bundle
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.webkit.WebChromeClient
import android.webkit.ConsoleMessage
import android.util.Log
import android.app.Activity
import com.pocketcluster.agent.agent.AgentService

class WebUIActivity : Activity() {

    companion object {
        private const val TAG = "WebUI"
    }

    private lateinit var webView: WebView

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        webView = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.allowFileAccess = true
            settings.allowContentAccess = true

            webViewClient = object : WebViewClient() {
                override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                    // Stay within the WebView for local agent URLs
                    val host = request.url.host
                    if (host == "localhost" || host == "127.0.0.1") return false
                    return false
                }
            }

            webChromeClient = object : WebChromeClient() {
                override fun onConsoleMessage(cm: ConsoleMessage): Boolean {
                    Log.d(TAG, "${cm.sourceId()}:${cm.lineNumber()}: ${cm.message()}")
                    return true
                }
            }
        }

        setContentView(webView)

        // Load the WebUI from the local agent
        val port = AgentService.currentNodeConfig?.httpPort ?: 7788
        webView.loadUrl("http://localhost:$port/")
    }

    override fun onBackPressed() {
        if (webView.canGoBack()) {
            webView.goBack()
        } else {
            super.onBackPressed()
        }
    }
}
