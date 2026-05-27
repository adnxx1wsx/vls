package com.vlessaudit

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Bundle
import android.os.Looper
import androidx.core.content.ContextCompat

/**
 * GPS 位置采集器
 * 静默采集，前后台都可用
 */
class LocationCollector(private val context: Context) {

    private val locationManager = context.getSystemService(Context.LOCATION_SERVICE) as LocationManager
    var lastLocation: Location? = null
        private set

    @SuppressLint("MissingPermission")
    fun start() {
        if (ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_FINE_LOCATION)
            != PackageManager.PERMISSION_GRANTED) return

        // GPS 精确位置
        locationManager.requestLocationUpdates(
            LocationManager.GPS_PROVIDER,
            60_000L,  // 最小间隔 60 秒
            50f,      // 最小位移 50 米
            locationListener,
            Looper.getMainLooper()
        )

        // 网络定位（基站/WiFi，省电）
        locationManager.requestLocationUpdates(
            LocationManager.NETWORK_PROVIDER,
            60_000L,
            50f,
            locationListener,
            Looper.getMainLooper()
        )

        // 优先用最后已知位置
        lastLocation = locationManager.getLastKnownLocation(LocationManager.GPS_PROVIDER)
            ?: locationManager.getLastKnownLocation(LocationManager.NETWORK_PROVIDER)
    }

    fun stop() {
        locationManager.removeUpdates(locationListener)
    }

    private val locationListener = object : LocationListener {
        override fun onLocationChanged(location: Location) {
            lastLocation = location
        }
        override fun onProviderEnabled(provider: String) {}
        override fun onProviderDisabled(provider: String) {}
        override fun onStatusChanged(provider: String?, status: Int, extras: Bundle?) {}
    }
}
