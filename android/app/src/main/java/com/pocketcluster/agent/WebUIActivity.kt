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
import android.graphics.Color
import android.view.View
import android.view.WindowInsetsController

class WebUIActivity : Activity() {

    companion object {
        private const val TAG = "WebUI"
        private const val PORT = 7788
    }

    private lateinit var webView: WebView

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // Edge-to-edge display
        window.decorView.systemUiVisibility = (
            View.SYSTEM_UI_FLAG_LAYOUT_STABLE
            or View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
            or View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
        )
        window.statusBarColor = Color.TRANSPARENT
        window.navigationBarColor = Color.TRANSPARENT

        webView = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.allowFileAccess = true
            settings.allowContentAccess = true

            webViewClient = object : WebViewClient() {
                override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
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
        webView.loadUrl("http://localhost:$PORT/")
    }

    override fun onBackPressed() {
        if (webView.canGoBack()) {
            webView.goBack()
        } else {
            super.onBackPressed()
        }
    }
}
