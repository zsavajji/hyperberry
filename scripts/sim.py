#!/usr/bin/env python3
"""Drives the simulated account of step 1.12 (engine/sim.go) on the Q10, over SSH.

    scripts/sim.py on            simulate (a file data/sim) and restart the engine
    scripts/sim.py link          the phone accepts the device the linking page shows
    scripts/sim.py msg N [text]  an incoming message in chat N of engine/sim_chats.go
    scripts/sim.py typing N     chat N types for 4 s (in a group, a member does)
    scripts/sim.py voice N [seconds]  an incoming voice note in chat N (5 s by default)
    scripts/sim.py photo N       an incoming photo in chat N, downloaded at once
    scripts/sim.py removed       the device removed on the phone
    scripts/sim.py off           the simulated chats removed, back to the real engine
    scripts/sim.py state         the engine's link body

Requests go to the engine's loopback port through an SSH tunnel. PHONE picks the host.
"""
import json
import os
import socket
import subprocess
import sys
import time

PHONE = os.environ.get("PHONE", "bb10rootx")
DATA = "/accounts/1000/appdata/com.zsavajji.hyperberry.*/data"


def ssh(cmd):
    return subprocess.run(["ssh", "-o", "BatchMode=yes", PHONE, cmd],
                          capture_output=True, text=True, check=True).stdout


def request(cmd, **args):
    info = json.loads(ssh(f"cat {DATA}/engine.json"))
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        local = s.getsockname()[1]
    tunnel = subprocess.Popen(["ssh", "-o", "BatchMode=yes", "-N", "-L",
                               f"{local}:127.0.0.1:{info['port']}", PHONE])
    try:
        for _ in range(50):
            try:
                conn = socket.create_connection(("127.0.0.1", local), timeout=10)
                break
            except OSError:
                time.sleep(0.1)
        else:
            sys.exit("the tunnel didn't open")
        f = conn.makefile("rw")

        def send(o):
            f.write(json.dumps(o) + "\n")
            f.flush()

        send({"@": "h", "cmd": "hello", "token": info["token"], "proto": 1})
        hello = json.loads(f.readline())
        if cmd == "hello":
            return hello.get("result")
        send({"@": "1", "cmd": cmd, **args})
        while True:  # events come in between
            m = json.loads(f.readline())
            if m.get("@") == "1":
                return m
    finally:
        tunnel.terminate()


def restart(touch):
    # The UI starts the engine again within a second while it's open; else when it opens.
    ssh(f"for d in {DATA}; do {'touch $d/sim; chmod 644 $d/sim' if touch else 'rm -f $d/sim'}; done; "
        "slay -f hyperberryd >/dev/null 2>&1; true")


def main(argv):
    if not argv:
        sys.exit(__doc__)
    cmd = argv[0]
    if cmd == "on":
        restart(True)
    elif cmd == "off":
        try:
            print(request("sim.removed"))
        except (subprocess.CalledProcessError, OSError, ValueError) as e:
            print("engine not reachable, simulated chats left:", e)
        restart(False)
    elif cmd == "state":
        print(request("hello"))
    elif cmd in ("link", "removed"):
        print(request("sim." + cmd))
    elif cmd == "msg" and len(argv) >= 2:
        print(request("sim.msg", n=int(argv[1]), text=" ".join(argv[2:])))
    elif cmd == "typing" and len(argv) >= 2:
        print(request("sim.typing", n=int(argv[1])))
    elif cmd == "voice" and len(argv) >= 2:
        print(request("sim.voice", n=int(argv[1]), seconds=int(argv[2]) if len(argv) > 2 else 0))
    elif cmd == "photo" and len(argv) >= 2:
        print(request("sim.photo", n=int(argv[1])))
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main(sys.argv[1:])
