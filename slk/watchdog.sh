#!/bin/bash
while true; do
    if ! pgrep -x "slkbank" > /dev/null; then
        echo "$(date) — slkbank crashed, restarting..." >> ~/Desktop/slk/watchdog.log
        DISPLAY=:0 XAUTHORITY=/home/michael-faraday/.Xauthority /home/michael-faraday/Desktop/slk/slkbank &
    fi
    sleep 10
done
