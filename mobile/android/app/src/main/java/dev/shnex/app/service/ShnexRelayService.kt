package dev.shnex.app.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import dev.shnex.app.MainActivity
import dev.shnex.app.security.KeystoreSecretVault
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import mobilebridge.Engine
import mobilebridge.Mobilebridge
import mobilebridge.NativePlatformBridge
import java.util.concurrent.atomic.AtomicInteger

data class CoreEndpoint(
    val apiBase: String,
    val wsBase: String,
    val token: String
)

class ShnexRelayService : Service() {

    companion object {
        const val CHANNEL_KEEPALIVE = "shnex_relay_keepalive"
        const val CHANNEL_ALERTS = "shnex_claude_alerts"
        const val NOTIF_ID_FOREGROUND = 1001
        const val EXTRA_JUMP_SESSION_ID = "jump_session_id"

        private val _endpointFlow = MutableStateFlow<CoreEndpoint?>(null)
        val endpointFlow: StateFlow<CoreEndpoint?> = _endpointFlow

        private val _bootErrorFlow = MutableStateFlow<String?>(null)
        val bootErrorFlow: StateFlow<String?> = _bootErrorFlow

        private val alertIdSeq = AtomicInteger(2000)

        fun ensureStarted(context: Context) {
            val intent = Intent(context, ShnexRelayService::class.java)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }
    }

    private var engine: Engine? = null
    private var connectivityManager: ConnectivityManager? = null
    private var networkCallback: ConnectivityManager.NetworkCallback? = null

    override fun onCreate() {
        super.onCreate()
        createNotificationChannels()
        val fgNotification = buildKeepaliveNotification("127.0.0.1 安全核心与 SSH/Claude 接力守护中")
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(
                NOTIF_ID_FOREGROUND,
                fgNotification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE
            )
        } else {
            startForeground(NOTIF_ID_FOREGROUND, fgNotification)
        }

        startGoCoreEngine()
        registerNetworkObserver()
    }

    private fun startGoCoreEngine() {
        if (engine != null) return
        try {
            val vault = KeystoreSecretVault(applicationContext)
            val bridge = object : NativePlatformBridge {
                override fun getSecret(key: String?): String {
                    if (key.isNullOrEmpty()) return ""
                    return vault.getSecret(key)
                }

                override fun setSecret(key: String?, value: String?) {
                    if (key.isNullOrEmpty()) return
                    vault.setSecret(key, value ?: "")
                }

                override fun notify(
                    title: String?,
                    body: String?,
                    category: String?,
                    sessionID: String?
                ) {
                    postClaudeAlertNotification(
                        title = title ?: "Shnex 通知",
                        body = body ?: "",
                        sessionId = sessionID ?: ""
                    )
                }
            }

            val started = Mobilebridge.startEngine(filesDir.absolutePath, bridge)
            engine = started
            _bootErrorFlow.value = null
            _endpointFlow.value = CoreEndpoint(
                apiBase = started.apiBaseUrl(),
                wsBase = started.wsBaseUrl(),
                token = started.authToken()
            )
        } catch (t: Throwable) {
            _bootErrorFlow.value = t.message ?: t.toString()
        }
    }

    private fun registerNetworkObserver() {
        val cm = getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return
        connectivityManager = cm
        val cb = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                engine?.notifyNetworkChanged("android_network_available")
            }

            override fun onCapabilitiesChanged(
                network: Network,
                networkCapabilities: NetworkCapabilities
            ) {
                val kind = when {
                    networkCapabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
                    networkCapabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
                    else -> "other"
                }
                engine?.notifyNetworkChanged("android_transport_$kind")
            }
        }
        try {
            cm.registerDefaultNetworkCallback(cb)
            networkCallback = cb
        } catch (_: Exception) {
        }
    }

    private fun createNotificationChannels() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

        val keepaliveChannel = NotificationChannel(
            CHANNEL_KEEPALIVE,
            "后台会话保活服务",
            NotificationManager.IMPORTANCE_LOW
        ).apply {
            description = "保持 SSH 与 Tailscale 接力会话在锁屏或切后台时不断开"
            setShowBadge(false)
        }

        val alertsChannel = NotificationChannel(
            CHANNEL_ALERTS,
            "Claude 待审批与完工通知",
            NotificationManager.IMPORTANCE_HIGH
        ).apply {
            description = "当远程 Claude CLI 等待确认时推送提醒"
            enableVibration(true)
        }

        nm.createNotificationChannel(keepaliveChannel)
        nm.createNotificationChannel(alertsChannel)
    }

    private fun buildKeepaliveNotification(contentText: String): Notification {
        val openIntent = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
        }
        val pi = PendingIntent.getActivity(
            this,
            0,
            openIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        return NotificationCompat.Builder(this, CHANNEL_KEEPALIVE)
            .setSmallIcon(android.R.drawable.stat_notify_sync_noanim)
            .setContentTitle("Shnex 接力核心运行中")
            .setContentText(contentText)
            .setOngoing(true)
            .setContentIntent(pi)
            .build()
    }

    private fun postClaudeAlertNotification(title: String, body: String, sessionId: String) {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val jumpIntent = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
            if (sessionId.isNotBlank()) {
                putExtra(EXTRA_JUMP_SESSION_ID, sessionId)
            }
        }
        val reqCode = alertIdSeq.incrementAndGet()
        val pi = PendingIntent.getActivity(
            this,
            reqCode,
            jumpIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        val notif = NotificationCompat.Builder(this, CHANNEL_ALERTS)
            .setSmallIcon(android.R.drawable.stat_sys_warning)
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(NotificationCompat.BigTextStyle().bigText(body))
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setAutoCancel(true)
            .setContentIntent(pi)
            .build()
        nm.notify(reqCode, notif)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startGoCoreEngine()
        return START_STICKY
    }

    override fun onDestroy() {
        networkCallback?.let { cb ->
            try {
                connectivityManager?.unregisterNetworkCallback(cb)
            } catch (_: Exception) {
            }
        }
        _endpointFlow.value = null
        try {
            engine?.stop()
        } catch (_: Exception) {
        }
        engine = null
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null
}
