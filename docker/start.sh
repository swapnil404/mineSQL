#!/bin/sh
set -e

TOTAL_MEM=$(awk '/MemTotal/ {printf "%.0f", $2/1024/1024}' /proc/meminfo 2>/dev/null || echo "4")
if [ "$TOTAL_MEM" -lt 2 ]; then
    MEM="1G"
else
    MEM=$(expr "$TOTAL_MEM" / 2)"G"
fi

echo "Starting mineSQL Minecraft server with ${MEM} heap..."

FIFO=/tmp/mcstdin
rm -f "$FIFO"
mkfifo "$FIFO"

java -Xms"${MEM}" -Xmx"${MEM}" -XX:+UseG1GC -jar paper.jar nogui < "$FIFO" &
MC_PID=$!

exec 3>"$FIFO"

echo "Waiting for server to be ready..."
READY=0
for i in $(seq 1 120); do
    if grep -q "Done (" logs/latest.log 2>/dev/null; then
        READY=1
        break
    fi
    sleep 2
done

if [ "$READY" -eq 0 ]; then
    echo "ERROR: Server failed to start within 4 minutes"
    exec 3>&-
    exit 1
fi

echo "Server ready. Setting gamerules..."
sleep 3

send_cmd() {
    echo "$1" >&3
    sleep 0.5
}

send_cmd "gamerule doWeatherCycle false"
send_cmd "gamerule doDaylightCycle false"
send_cmd "gamerule doFireTick false"
send_cmd "gamerule doMobSpawning false"
send_cmd "gamerule randomTickSpeed 0"
send_cmd "time set 6000"
send_cmd "gamerule announceAdvancements false"
send_cmd "gamerule sendCommandFeedback false"

echo "Gamerules set. mineSQL storage world is ready."
echo ""
echo "  Minecraft server   : localhost:${MC_SERVER_PORT:-25565}"
echo "  Plugin TCP (HAL)   : localhost:${MC_PLUGIN_PORT:-25576}"

exec 3>&-
wait $MC_PID
