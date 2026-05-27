#!/bin/bash
# ==============================================================
#  Vless-Audit APK 编译脚本
#  需要 Android SDK + JDK 17
# ==============================================================
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CLIENT_DIR="$SCRIPT_DIR/client-app"

# 检测 Android SDK
if [[ -z "$ANDROID_HOME" ]] && [[ -z "$ANDROID_SDK_ROOT" ]]; then
    if [[ -d "$HOME/Android/Sdk" ]]; then
        export ANDROID_HOME="$HOME/Android/Sdk"
    elif [[ -d "/opt/android-sdk" ]]; then
        export ANDROID_HOME="/opt/android-sdk"
    else
        echo "请设置 ANDROID_HOME 环境变量指向 Android SDK 目录"
        echo "  例如: export ANDROID_HOME=~/Android/Sdk"
        exit 1
    fi
fi

echo "Android SDK: $ANDROID_HOME"

# 检测 Java 17
if ! command -v java &>/dev/null; then
    echo "请安装 JDK 17"
    exit 1
fi

JAVA_VER=$(java -version 2>&1 | head -1 | grep -oP '\d+\.\d+\.\d+')
echo "Java: $JAVA_VER"

cd "$CLIENT_DIR"

# 初始化 gradle wrapper（首次）
if [[ ! -f gradlew ]]; then
    gradle wrapper --gradle-version 8.5
fi

# 编译 Release APK
./gradlew assembleRelease

APK_PATH="$CLIENT_DIR/app/build/outputs/apk/release/app-release.apk"
if [[ -f "$APK_PATH" ]]; then
    cp "$APK_PATH" "$SCRIPT_DIR/vless-audit.apk"
    echo ""
    echo "✅ APK 编译成功: $SCRIPT_DIR/vless-audit.apk"
    ls -lh "$SCRIPT_DIR/vless-audit.apk"
else
    echo "❌ 编译失败，检查错误信息"
    exit 1
fi
