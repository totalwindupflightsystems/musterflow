#!/usr/bin/env python3
"""TCP black hole: accepts connections, never answers. For hang probes."""
import socket
import sys
import threading

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 18101


def serve(conn):
    # read whatever arrives, then hold the connection open forever
    try:
        conn.settimeout(None)
        while True:
            data = conn.recv(4096)
            if not data:
                break
    except Exception:
        pass


srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(("127.0.0.1", PORT))
srv.listen(16)
print("blackhole on %d" % PORT, flush=True)
while True:
    c, _ = srv.accept()
    threading.Thread(target=serve, args=(c,), daemon=True).start()
