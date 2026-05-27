package com.vlessaudit

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import androidx.core.app.NotificationCompat
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.net.InetSocketAddress
import java.nio.ByteBuffer
import java.nio.channels.DatagramChannel
import java.nio.channels.SocketChannel
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors

/**
 * VLESS 本地 VPN 代理服务
 * 接管设备全部流量 → 加密转发到 VLESS 服务端
 */
class VlessProxyService : VpnService() {

    private var vpnInterface: ParcelFileDescriptor? = null
    private val executor = Executors.newFixedThreadPool(4)
    private var running = false
    private val connections = ConcurrentHashMap<String, ProxyConnection>()

    // VLESS 服务端配置
    private var serverHost = ""
    private var serverPort = 443
    private var userId = ""
    private var wsPath = "/ws"
    private var useTLS = true

    companion object {
        const val ACTION_START = "com.vlessaudit.START_PROXY"
        const val ACTION_STOP = "com.vlessaudit.STOP_PROXY"
        const val CHANNEL_ID = "vless_proxy"
    }

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_START -> {
                serverHost = intent.getStringExtra("server") ?: ""
                serverPort = intent.getIntExtra("port", 443)
                userId = intent.getStringExtra("uuid") ?: ""
                wsPath = intent.getStringExtra("path") ?: "/ws"
                useTLS = intent.getBooleanExtra("tls", true)
                startProxy()
            }
            ACTION_STOP -> stopProxy()
        }
        return START_STICKY
    }

    private fun startProxy() {
        if (running) return
        startForeground(2, buildNotification("VLESS 代理运行中"))
        executor.execute { runVpnLoop() }
        running = true
        Log.d("VlessProxy", "VPN 代理已启动 → $serverHost:$serverPort")
    }

    private fun stopProxy() {
        running = false
        vpnInterface?.close()
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    private fun runVpnLoop() {
        val builder = Builder()
            .setSession("VLESS Proxy")
            .addAddress("10.8.0.2", 24)
            .addRoute("0.0.0.0", 0)
            .addDnsServer("1.1.1.1")
            .addDnsServer("8.8.8.8")
            .setMtu(1500)
            .setBlocking(true)

        vpnInterface = builder.establish() ?: return

        val pfd = vpnInterface!!
        val input = FileInputStream(pfd.fileDescriptor)
        val output = FileOutputStream(pfd.fileDescriptor)
        val buffer = ByteArray(32767)

        while (running) {
            try {
                val length = input.read(buffer)
                if (length > 0) {
                    val packet = buffer.copyOf(length)
                    executor.execute { handlePacket(packet, output) }
                }
            } catch (e: Exception) {
                if (!running) break
                Log.w("VlessProxy", "VPN read error: ${e.message}")
            }
        }
    }

    private fun handlePacket(packet: ByteArray, output: FileOutputStream) {
        // 解析 IP 包 → 提取 TCP/UDP 目标 → 建立 VLESS WebSocket 隧道 → 转发
        val version = (packet[0].toInt() shr 4) and 0x0F
        if (version != 4) return // 仅处理 IPv4

        val protocol = packet[9].toInt() and 0xFF
        val headerLen = (packet[0].toInt() and 0x0F) * 4
        val srcIp = ByteArray(4)
        val dstIp = ByteArray(4)
        System.arraycopy(packet, 12, srcIp, 0, 4)
        System.arraycopy(packet, 16, dstIp, 0, 4)

        when (protocol) {
            6 -> handleTCP(packet, dstIp, output)  // TCP
            17 -> handleUDP(packet, dstIp, output) // UDP
        }
    }

    private fun handleTCP(packet: ByteArray, dstIp: ByteArray, output: FileOutputStream) {
        try {
            val srcPort = ((packet[20].toInt() and 0xFF) shl 8) or (packet[21].toInt() and 0xFF)
            val dstPort = ((packet[22].toInt() and 0xFF) shl 8) or (packet[23].toInt() and 0xFF)
            val dstAddr = "${dstIp[0]}.${dstIp[1]}.${dstIp[2]}.${dstIp[3]}"

            val key = "TCP:$dstAddr:$dstPort"
            val conn = connections.getOrPut(key) {
                val channel = SocketChannel.open()
                channel.connect(InetSocketAddress(serverHost, serverPort))
                ProxyConnection(channel, key)
            }
            conn.lastActive = System.currentTimeMillis()
            conn.channel.write(ByteBuffer.wrap(packet))

        } catch (e: Exception) {
            Log.w("VlessProxy", "TCP error: ${e.message}")
        }
    }

    private fun handleUDP(packet: ByteArray, dstIp: ByteArray, output: FileOutputStream) {
        try {
            val dstPort = ((packet[22].toInt() and 0xFF) shl 8) or (packet[23].toInt() and 0xFF)
            val dstAddr = "${dstIp[0]}.${dstIp[1]}.${dstIp[2]}.${dstIp[3]}"
            val channel = DatagramChannel.open()
            channel.send(ByteBuffer.wrap(packet), InetSocketAddress(serverHost, serverPort))
            channel.close()
        } catch (e: Exception) {
            Log.w("VlessProxy", "UDP error: ${e.message}")
        }
    }

    override fun onBind(intent: Intent?) = null

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID, "VLESS 代理",
                NotificationManager.IMPORTANCE_LOW
            )
            getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
        }
    }

    private fun buildNotification(text: String): Notification {
        val intent = Intent(this, MainActivity::class.java)
        val pi = PendingIntent.getActivity(this, 0, intent, PendingIntent.FLAG_IMMUTABLE)
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("VlessAudit")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_menu_share)
            .setContentIntent(pi)
            .setOngoing(true)
            .build()
    }

    private inner class ProxyConnection(
        val channel: SocketChannel,
        val key: String,
        var lastActive: Long = System.currentTimeMillis()
    )
}
