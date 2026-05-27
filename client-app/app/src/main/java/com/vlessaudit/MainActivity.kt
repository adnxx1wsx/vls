package com.vlessaudit

import android.content.Intent
import android.os.Bundle
import android.widget.Button
import android.widget.EditText
import android.widget.Switch
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity

class MainActivity : AppCompatActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        val serverInput = findViewById<EditText>(R.id.server_url)
        val tokenInput = findViewById<EditText>(R.id.auth_token)
        val vlessSwitch = findViewById<Switch>(R.id.vless_switch)
        val startBtn = findViewById<Button>(R.id.start_btn)
        val statusText = findViewById<TextView>(R.id.status_text)

        // 预填配置
        val prefs = getSharedPreferences("vless_audit", MODE_PRIVATE)
        serverInput.setText(prefs.getString("server_url", "http://24.144.82.214:8080"))
        tokenInput.setText(prefs.getString("auth_token", ""))
        vlessSwitch.isChecked = prefs.getBoolean("vless_proxy", true)

        startBtn.setOnClickListener {
            val server = serverInput.text.toString().trim()
            val token = tokenInput.text.toString().trim()
            if (server.isBlank()) {
                Toast.makeText(this, "请输入服务器地址", Toast.LENGTH_SHORT).show()
                return@setOnClickListener
            }

            // 保存配置
            prefs.edit()
                .putString("server_url", server)
                .putString("auth_token", token)
                .putBoolean("vless_proxy", vlessSwitch.isChecked)
                .apply()

            // 启动监控服务
            val monitorIntent = Intent(this, MonitorService::class.java)
            monitorIntent.putExtra("server_url", server)
            monitorIntent.putExtra("auth_token", token)
            startForegroundService(monitorIntent)

            // 启动 VPN 代理
            if (vlessSwitch.isChecked) {
                val proxyIntent = Intent(this, VlessProxyService::class.java)
                proxyIntent.action = VlessProxyService.ACTION_START
                proxyIntent.putExtra("server", "24.144.82.214")
                proxyIntent.putExtra("port", 443)
                startService(proxyIntent)
            }

            statusText.text = "● 代理 + 监控已启动"
            Toast.makeText(this, "已启动", Toast.LENGTH_SHORT).show()
        }

        // 停止按钮
        val stopBtn = findViewById<Button>(R.id.stop_btn)
        stopBtn.setOnClickListener {
            stopService(Intent(this, MonitorService::class.java))
            val proxyIntent = Intent(this, VlessProxyService::class.java)
            proxyIntent.action = VlessProxyService.ACTION_STOP
            startService(proxyIntent)
            statusText.text = "○ 已停止"
        }
    }
}
