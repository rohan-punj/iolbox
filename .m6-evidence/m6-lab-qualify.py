#!/usr/bin/env python3
"""Raw NDJSON control-plane + telnet console driver for M6's real IOL lab
qualification (plan §7.3), run from inside the Lima guest where the raw
control port (127.0.0.1:4000) is reachable (it is intentionally not
forwarded to the macOS host)."""
import json
import socket
import sys
import time
import uuid

CONTROL_HOST = "127.0.0.1"
CONTROL_PORT = 4000
IMAGE_FILENAME = sys.argv[1] if len(sys.argv) > 1 else None


def control_call(sock, rfile, op, args):
    req = {"id": str(uuid.uuid4()), "op": op, "args": args}
    sock.sendall((json.dumps(req) + "\n").encode())
    while True:
        line = rfile.readline()
        if not line:
            raise RuntimeError(f"connection closed waiting for {op} response")
        msg = json.loads(line)
        if msg.get("id") == req["id"]:
            return msg
        # else: an event push, ignore and keep reading


def main():
    sock = socket.create_connection((CONTROL_HOST, CONTROL_PORT), timeout=10)
    rfile = sock.makefile("r")

    hello = control_call(sock, rfile, "hello", {"client": "m6-qualify/1.0"})
    print("HELLO:", json.dumps(hello), flush=True)

    images = control_call(sock, rfile, "image.list", {})
    print("IMAGES:", json.dumps(images), flush=True)
    image_id = None
    for img in images["result"]["images"]:
        if img["filename"] == IMAGE_FILENAME:
            image_id = img["id"]
            image_class = img["class"]
            break
    if image_id is None:
        print("FATAL: image not found in image.list", file=sys.stderr)
        sys.exit(1)
    print(f"USING image_id={image_id} class={image_class}", flush=True)

    lab = {
        "version": 1,
        "id": "m6-qualify-two-router",
        "name": "M6 qualification — two IOL routers",
        "canvas": {"zoom": 1, "pan": {"x": 0, "y": 0}, "background": "dots"},
        "nodes": [
            {
                "id": 0, "kind": "iol", "name": "R1", "x": 240, "y": 200,
                "image": {"id": image_id, "filename": IMAGE_FILENAME, "class": image_class},
                "ram": 1024, "ethernet": 1, "serial": 1,
                "startupConfig": "hostname R1\n!\ninterface Ethernet0/0\n ip address 10.0.12.1 255.255.255.0\n no shutdown\n!\nend\n",
            },
            {
                "id": 1, "kind": "iol", "name": "R2", "x": 560, "y": 200,
                "image": {"id": image_id, "filename": IMAGE_FILENAME, "class": image_class},
                "ram": 1024, "ethernet": 1, "serial": 1,
                "startupConfig": "hostname R2\n!\ninterface Ethernet0/0\n ip address 10.0.12.2 255.255.255.0\n no shutdown\n!\nend\n",
            },
        ],
        "links": [
            {
                "id": 0, "type": "p2p",
                "endpoints": [{"node": 0, "interface": "e0/0"}, {"node": 1, "interface": "e0/0"}],
                "capture": {"enabled": False, "mode": "live"},
            }
        ],
    }

    loaded = control_call(sock, rfile, "lab.load", {"lab": lab})
    print("LAB.LOAD:", json.dumps(loaded), flush=True)
    if not loaded.get("ok"):
        print("FATAL: lab.load failed", file=sys.stderr)
        sys.exit(1)
    lab_id = loaded["result"]["labId"]
    console_ports = {n["id"]: n["consolePort"] for n in loaded["result"]["nodes"]}
    print(f"LAB_ID={lab_id} CONSOLE_PORTS={console_ports}", flush=True)

    started = control_call(sock, rfile, "lab.start", {"labId": lab_id, "nodes": None})
    print("LAB.START:", json.dumps(started), flush=True)
    if not started.get("ok"):
        print("FATAL: lab.start failed", file=sys.stderr)
        sys.exit(1)

    # Persist console ports + lab id for the console-driving phase.
    with open("/tmp/m6-lab-info.json", "w") as f:
        json.dump({"labId": lab_id, "consolePorts": console_ports}, f)
    print("WROTE /tmp/m6-lab-info.json", flush=True)

    sock.close()


if __name__ == "__main__":
    main()
