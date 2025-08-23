## Moonlight Server

![Moonlight Server](images/banner.png)

A lightweight HTTP JSON proxy that selects a registered client by region and forwards your payload, returning the client's response as-is. Now supports both HTTP and WebSocket communication for enhanced real-time capabilities.

### Features
- Token-gated client registration (heartbeats)
- Region-aware client selection with hierarchical fallback
- Simple load balancing by recency and latency
- Server-assigned job IDs (incoming IDs ignored)
- Payload-only proxy: only `payload` is sent to the client
- **NEW**: WebSocket support for real-time communication
- **NEW**: Automatic fallback from WebSocket to HTTP if needed

### Quick start
```bash
make build
./build/moonlight-server
# or
make run
```
Copy config (example at repo root) to the default path:
```bash
sudo mkdir -p /etc/moonlight
sudo cp mls.json /etc/moonlight/mls.json
```
The server listens on `:8080`.

### Configuration
Minimal `/etc/moonlight/mls.json`:
```json
{
  "tokens": ["supersecrettoken1"],
  "retry_count": 3,
  "port": 8080,
  "ws": {
    "enabled": true,
    "path": "/ws",
    "max_connections": 1000,
    "read_timeout": 60,
    "write_timeout": 10
  },
  "html": {
    "enabled": true,
    "static_path": "/static",
    "index_path": "/",
    "dashboard_path": "/dashboard"
  },
  "region_hierarchy": {
    "us-east-1": "usa",
    "usa": "northamerica",
    "northamerica": "global"
  }
}
```

#### Configuration Options

**WebSocket Configuration (`ws`):**
- `enabled`: Enable/disable WebSocket functionality (default: true)
- `path`: WebSocket endpoint path (default: "/ws")
- `max_connections`: Maximum concurrent WebSocket connections (default: 1000)
- `read_timeout`: Read timeout in seconds (default: 60)
- `write_timeout`: Write timeout in seconds (default: 10)

**HTML Configuration (`html`):**
- `enabled`: Enable/disable HTML dashboard (default: true)
- `static_path`: Static files path (default: "/static")
- `index_path`: Index page path (default: "/")
- `dashboard_path`: Dashboard path (default: "/dashboard")

### HTTP API
All endpoints use/return JSON.

- POST `/client/heartbeat`
  - Body:
    ```json
    { "ip": "1.2.3.4", "node_id": "node-1", "token": "supersecrettoken1", "region": "us-east-1", "port": 3000 }
    ```
  - Responses: 200 "registered"; 401 invalid token; 405 wrong method

- POST `/task/request`
  - Body:
    ```json
    { "region": "usa", "payload": {"any": "json"} }
    ```
  - Behavior: server assigns a job ID, forwards only `payload` to the chosen client, and returns that client's response body/status.

### WebSocket API
Connect to `ws://server:port/ws` for real-time communication.

#### Message Types

**Client Registration:**
```json
{
  "type": "register",
  "payload": {
    "ip": "1.2.3.4",
    "node_id": "node-1", 
    "token": "supersecrettoken1",
    "region": "us-east-1",
    "port": 3000
  }
}
```

**Heartbeat:**
```json
{
  "type": "heartbeat",
  "payload": {
    "timestamp": "2024-01-01T12:00:00Z"
  }
}
```

**Task (from server to client):**
```json
{
  "type": "task",
  "task_id": "abc123",
  "payload": {"any": "json"}
}
```

**Task Response (from client to server):**
```json
{
  "type": "task_response",
  "payload": {
    "task_id": "abc123",
    "response": {"result": "data"},
    "status": 200,
    "headers": {"Content-Type": "application/json"}
  }
}
```

### Clients

#### Node.js Client
```bash
cd clients
npm install
node websocket-client.js
```

#### Python Client
```bash
cd clients
pip install -r requirements.txt
python websocket-client.py
```

#### Legacy HTTP Client
Example Node.js HTTP client is in `clients/node-client.js`. It heartbeats automatically and exposes `/work` which echoes the body.

### HTML Dashboard

When HTML is enabled in the configuration, the server provides a web-based dashboard at the configured paths:

- **Main Dashboard**: `http://server:port/` or `http://server:port/dashboard`
- **Static Files**: `http://server:port/static/`

The dashboard provides:
- Real-time server status monitoring
- WebSocket connection status
- Active client count
- Task processing statistics
- Interactive task testing interface
- Live WebSocket connection to the server

To access the dashboard, simply open your browser and navigate to the server URL.

### Communication Flow

1. **WebSocket (Preferred)**: Clients connect via WebSocket for real-time communication
2. **HTTP Fallback**: If WebSocket fails, the server automatically falls back to HTTP
3. **Task Processing**: Tasks are sent via WebSocket when possible, with automatic response handling
4. **Load Balancing**: Server selects the best available client based on region, recency, and latency

### Notes
- "global", "default", or empty region match any client.
- If multiple clients match, the most recently seen with the lowest average latency is preferred.
- WebSocket connections are automatically managed with reconnection logic.
- Task responses are handled asynchronously with proper timeout handling.

