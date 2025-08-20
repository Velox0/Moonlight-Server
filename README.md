## Moonlight Server

![Moonlight Server](images/banner.png)

A lightweight HTTP JSON proxy that selects a registered client by region and forwards your payload, returning the client's response as-is.

### Features
- Token-gated client registration (heartbeats)
- Region-aware client selection with hierarchical fallback
- Simple load balancing by recency and latency
- Server-assigned job IDs (incoming IDs ignored)
- Payload-only proxy: only `payload` is sent to the client `/work`

### Quick start
```bash
make build
./build/app
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
  "region_hierarchy": {
    "us-east-1": "usa",
    "usa": "northamerica",
    "northamerica": "global"
  }
}
```

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
  - Behavior: server assigns a job ID, forwards only `payload` to the chosen client's `/work`, and returns that client's response body/status.

### Client
Example Node.js client is in `clients/node-client.js`. It heartbeats automatically and exposes `/work` which echoes the body.

### Notes
- "global", "default", or empty region match any client.
- If multiple clients match, the most recently seen with the lowest average latency is preferred.

