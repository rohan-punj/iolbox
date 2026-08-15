#!/usr/bin/env python3
import json, socket, uuid


def call(sock, rfile, op, args):
    req = {"id": str(uuid.uuid4()), "op": op, "args": args}
    sock.sendall((json.dumps(req) + "\n").encode())
    while True:
        line = rfile.readline()
        msg = json.loads(line)
        if msg.get("id") == req["id"]:
            return msg


sock = socket.create_connection(("127.0.0.1", 4000), timeout=10)
rfile = sock.makefile("r")

docs = call(sock, rfile, "lab.listDocs", {})
print("RAW_DOCS:", json.dumps(docs)[:2000])
ids = [l.get("id") if isinstance(l, dict) else l for l in docs["result"]["labs"]]
print("SAVED_LAB_IDS:", ids)

target = "seed-2-routers"
doc = call(sock, rfile, "lab.getDoc", {"labId": target})
lab_json = doc["result"]["lab"]
print(f"LOADED_DOC ok={doc['ok']} nodes={len(lab_json.get('nodes', []))}")

loaded = call(sock, rfile, "lab.load", {"lab": lab_json})
print("LAB.LOAD:", json.dumps(loaded))
if loaded["ok"]:
    lab_id = loaded["result"]["labId"]
    started = call(sock, rfile, "lab.start", {"labId": lab_id, "nodes": None})
    print("LAB.START:", json.dumps(started))

sock.close()
