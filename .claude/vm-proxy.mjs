// Plain TCP proxy: localhost:4174 -> 192.168.111.152:4001 (iolbox runtime VM).
// HTTP and WebSocket both pass through untouched, so the preview tools can
// drive the REAL VM-served GUI. Used by the "iolbox-vm-proxy" launch config.
import net from "node:net";

const LISTEN = 4174;
const TARGET_HOST = "192.168.111.152";
const TARGET_PORT = 4001;

const server = net.createServer((client) => {
  const upstream = net.connect(TARGET_PORT, TARGET_HOST);
  client.pipe(upstream);
  upstream.pipe(client);
  const kill = () => {
    client.destroy();
    upstream.destroy();
  };
  client.on("error", kill);
  upstream.on("error", kill);
});

server.listen(LISTEN, "127.0.0.1", () => {
  console.log(`iolbox vm proxy: 127.0.0.1:${LISTEN} -> ${TARGET_HOST}:${TARGET_PORT}`);
});
