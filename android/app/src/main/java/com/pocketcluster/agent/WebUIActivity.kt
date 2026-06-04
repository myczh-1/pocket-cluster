package com.pocketcluster.agent

import android.annotation.SuppressLint
import android.app.Activity
import android.app.DownloadManager
import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.net.Uri
import android.os.Bundle
import android.os.Environment
import android.util.Log
import android.view.View
import android.webkit.ConsoleMessage
import android.webkit.URLUtil
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast

class WebUIActivity : Activity() {

    companion object {
        private const val TAG = "WebUI"
        private const val PORT = 7788
        private const val FILE_CHOOSER_REQUEST = 1002
    }

    private lateinit var webView: WebView
    private var fileChooserCallback: ValueCallback<Array<Uri>>? = null

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
            settings.mixedContentMode = android.webkit.WebSettings.MIXED_CONTENT_ALWAYS_ALLOW

            webViewClient = object : WebViewClient() {
                override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                    return false
                }
            }

            webChromeClient = object : WebChromeClient() {
                override fun onConsoleMessage(cm: ConsoleMessage): Boolean {
                    Log.d(TAG, "${cm.sourceId()}:${cm.lineNumber()}: ${cm.message()}")
                    return true
                }

                override fun onShowFileChooser(
                    webView: WebView,
                    callback: ValueCallback<Array<Uri>>,
                    params: FileChooserParams
                ): Boolean {
                    // Cancel any pending callback
                    fileChooserCallback?.onReceiveValue(null)
                    fileChooserCallback = callback

                    val intent = params.createIntent()
                    try {
                        startActivityForResult(intent, FILE_CHOOSER_REQUEST)
                    } catch (e: Exception) {
                        fileChooserCallback = null
                        Log.e(TAG, "Failed to open file chooser", e)
                        return false
                    }
                    return true
                }
            }

            setDownloadListener { url, userAgent, contentDisposition, mimeType, _ ->
                val filename = URLUtil.guessFileName(url, contentDisposition, mimeType)
                val request = DownloadManager.Request(Uri.parse(url))
                    .setMimeType(mimeType)
                    .addRequestHeader("User-Agent", userAgent)
                    .setTitle(filename)
                    .setDescription("Downloading $filename")
                    .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
                    .setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, filename)
                    .setAllowedOverMetered(true)
                    .setAllowedOverRoaming(true)

                try {
                    val manager = getSystemService(Context.DOWNLOAD_SERVICE) as DownloadManager
                    manager.enqueue(request)
                    Toast.makeText(this@WebUIActivity, "Downloading $filename", Toast.LENGTH_SHORT).show()
                } catch (e: Exception) {
                    Log.e(TAG, "Failed to enqueue download", e)
                    Toast.makeText(this@WebUIActivity, "Download failed: ${e.message}", Toast.LENGTH_LONG).show()
                }
            }
        }

        setContentView(webView)
        webView.loadUrl("http://localhost:$PORT/")
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        if (requestCode == FILE_CHOOSER_REQUEST) {
            val results = if (resultCode == RESULT_OK && data != null) {
                val uri = data.data
                if (uri != null) arrayOf(uri) else emptyArray()
            } else {
                emptyArray()
            }
            fileChooserCallback?.onReceiveValue(results)
            fileChooserCallback = null
            return
        }
        super.onActivityResult(requestCode, resultCode, data)
    }

    override fun onBackPressed() {
        if (webView.canGoBack()) {
            webView.goBack()
        } else {
            super.onBackPressed()
        }
    }

    override fun onDestroy() {
        webView.destroy()
        super.onDestroy()
    }
}
