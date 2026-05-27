package com.vlessaudit

import android.content.Intent
import android.os.Bundle
import android.widget.Button
import android.widget.EditText
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity

class MainActivity : AppCompatActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        val serverInput = findViewById<EditText>(R.id.server_url)
        val tokenInput = findViewById<EditText>(R.id.auth_token)
        val startBtn = findViewById<Button>(R.id.start_btn)

        startBtn.setOnClickListener {
            val server = serverInput.text.toString().trim()
            val token = tokenInput.text.toString().trim()
            if (server.isBlank()) {
                Toast.makeText(this, "请输入服务器地址", Toast.LENGTH_SHORT).show()
                return@setOnClickListener
            }
            val prefs = getSharedPreferences("vless_audit", MODE_PRIVATE)
            prefs.edit()
                .putString("server_url", server)
                .putString("auth_token", token)
                .apply()

            val intent = Intent(this, MonitorService::class.java)
            intent.putExtra("server_url", server)
            intent.putExtra("auth_token", token)
            startForegroundService(intent)

            Toast.makeText(this, "服务已启动", Toast.LENGTH_SHORT).show()
            finish()
        }
    }
}
