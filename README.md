## Moonlight Server

![Moonlight Server](images/banner.png)

A lightweight HTTP JSON proxy with gRPC bidirectional streaming support for mobile and wifi clients. The server selects registered clients by region and forwards payloads, returning responses as-is.

### Features
- **Dual Protocol Support**: HTTP REST API and gRPC bidirectional streaming
- **Mobile-Friendly**: Optimized for mobile networks with reconnection, keepalives, and compression
- **Token-gated Registration**: Secure client authentication with configurable tokens
- **Region-aware Selection**: Hierarchical region fallback system
- **Load Balancing**: Client selection by recency and latency
- **Server-assigned Job IDs**: Automatic ID generation for tracking
- **Payload-only Proxy**: Only `payload` forwarded to clients

### Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                    Moonlight Server                            │
│  ┌────────────────────┐        ┌─────────────────────────────┐ │
│  │   HTTP Server      │        │        gRPC Server          │ │
│  │   Port: 8080       │        │        Port: 8081           │ │
│  │                    │        │                             │ │
│  │ • /task/request    │        │ • Bidirectional Streaming   │ │
│  │ • /client/heartbeat│        │ • Mobile Optimized          │ │
│  │ • /status          │        │ • Auto-reconnection         │ │
│  └────────────────────┘        └─────────────────────────────┘ │
└────────────────────────────────────────────────────────────────┘
                             │
                  ┌──────────┼──────────┐
                  │          │          │
          ┌───────▼────┐ ┌───▼────┐ ┌───▼──────┐
          │HTTP Client │ │ gRPC   │ │ Mobile   │
          │(Legacy)    │ │ Client │ │ gRPC     │
          │            │ │        │ │ Client   │
          └────────────┘ └────────┘ └──────────┘
```

### Quick Start

#### 1. Build and Run
```bash
# Setup development environment
make setup

# Build with gRPC support
make build

# Or run directly
make run
```

#### 2. Configuration
Copy the sample configuration:
```bash
sudo mkdir -p /etc/moonlight
sudo cp mls.json /etc/moonlight/mls.json
```

Edit `/etc/moonlight/mls.json`:
```json
{
  "tokens": ["your-secret-token"],
  "port": 8080,
  "enable_grpc": true,
  "grpc_port": 8081,
  "grpc_config": {
    "max_message_size": 8388608,
    "keepalive_time": 60,
    "keepalive_timeout": 10,
    "enable_reflection": false,
    "enable_compression": true
  },
  "region_hierarchy": {
    "us-east-1": "usa",
    "usa": "northamerica",
    "northamerica": "global"
  }
}
```

### HTTP API

All endpoints use/return JSON.

#### POST `/client/heartbeat`
Register/update HTTP client:
```json
{
  "ip": "1.2.3.4",
  "node_id": "node-1", 
  "token": "your-secret-token",
  "region": "us-east-1",
  "port": 3000
}
```

**Responses:**
- `200 "registered"` - Success
- `401` - Invalid token  
- `405` - Wrong method

#### POST `/task/request`
Submit task for processing:
```json
{
  "region": "usa",
  "payload": {"any": "json data"}
}
```

**Behavior:**
- Server assigns job ID
- Selects best client (HTTP or gRPC)
- Forwards only `payload` to client
- Returns client's response

#### GET `/status`
Server status information:
```json
{
  "http_clients": 2,
  "grpc_clients": 3, 
  "total_clients": 5,
  "regions": ["us-east-1", "europe", "asia"]
}
```

### gRPC API

The gRPC service uses bidirectional streaming for real-time communication.

#### Service Definition
```protobuf
service Moonlight {
  rpc Connect(stream TaskStreamMessage) returns (stream TaskStreamMessage);
}
```

#### Message Types
```protobuf
message ClientHello {
  string ip = 1;
  string node_id = 2;
  string token = 3;
  string region = 4;
  int32 port = 5;
}

message Task {
  string id = 1;
  string region = 2;
  bytes payload = 3;
}

message TaskResult {
  string id = 1;
  bytes data = 2;
  string err = 3;
}
```

### Client Examples

#### Node.js gRPC Client
```javascript
const client = new MoonlightGRPCClient('localhost:8081', {
    ip: '192.168.1.100',
    nodeId: 'mobile-client-001',
    token: 'your-secret-token',
    region: 'us-east-1'
});

client.connect();
```

#### Python gRPC Client
```python
client = MoonlightGRPCClient('localhost:8081', {
    'ip': '192.168.1.100',
    'node_id': 'python-client-001', 
    'token': 'your-secret-token',
    'region': 'us-east-1'
})

client.connect()
```

See `clients/` directory for complete examples.

### Region Hierarchy

Clients are selected based on region with hierarchical fallback:

1. **Exact Match**: `us-east-1` → exact region match
2. **Parent Region**: `us-east-1` → `usa` → `northamerica` 
3. **Global Fallback**: `northamerica` → `global`
4. **Any Available**: If no regional match, use any valid client

Special regions:
- `global`, `default`, or empty: matches any client
- Hierarchy configured in `region_hierarchy` section

### Mobile Network Optimization

gRPC clients include mobile-friendly features:

- **Connection Persistence**: Long-lived bidirectional streams
- **Automatic Reconnection**: Exponential backoff with max attempts  
- **Keepalives**: Configurable heartbeat intervals
- **Message Compression**: Reduces bandwidth usage
- **Large Message Support**: Configurable size limits
- **Network-aware Timeouts**: Suitable for mobile networks

### Development

#### Prerequisites
- Go 1.21+
- Protocol Buffers compiler (`protoc`)
- Node.js (for JavaScript clients)
- Python 3.8+ (for Python clients)

#### Generate Protobuf Code
```bash
# All languages
make proto

# Specific language  
make proto-go
make proto-js
make proto-python
```

#### Development Server
```bash
# Auto-restart on changes
make dev

# Docker development
make docker-run
```

#### Testing
```bash
# Run tests
make test

# Format code
make fmt

# Lint code  
make lint
```

### Deployment

#### System Install
```bash
# Install to /opt/moonlight
make install

# Create systemd service
make systemd-service
sudo systemctl enable moonlight
sudo systemctl start moonlight
```

#### Docker
```bash
# Build image
make docker

# Run container  
docker run -p 8080:8080 -p 8081:8081 \
  -v /etc/moonlight:/etc/moonlight:ro \
  moonlight-server
```

#### Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `tokens` | []string | - | Valid authentication tokens |
| `port` | int | 8080 | HTTP server port |
| `enable_grpc` | bool | true | Enable gRPC server |
| `grpc_port` | int | 8081 | gRPC server port |
| `grpc_config.max_message_size` | int | 4MB | Max message size |
| `grpc_config.keepalive_time` | int | 60 | Keepalive interval (seconds) |
| `grpc_config.keepalive_timeout` | int | 10 | Keepalive timeout (seconds) |
| `grpc_config.enable_reflection` | bool | false | Enable gRPC reflection |
| `grpc_config.enable_compression` | bool | true | Enable compression |
| `region_hierarchy` | map | - | Region parent mapping |

### Security Considerations

- **Token Authentication**: All clients must provide valid tokens
- **gRPC Reflection**: Disabled by default in production
- **Network Security**: Use TLS in production (configure via gRPC options)
- **Resource Limits**: Configure message size limits appropriately
- **Access Control**: Restrict network access to trusted clients

### Troubleshooting

#### Common Issues

**gRPC Connection Failures:**
```bash
# Check server is listening
netstat -tlnp | grep 8081

# Test basic connectivity  
grpcurl -plaintext localhost:8081 list
```

**Mobile Client Reconnection:**
- Check keepalive settings
- Verify network stability
- Monitor reconnection logs
- Adjust timeouts for network conditions

**Region Selection:**
```bash
# Check active regions
curl http://localhost:8080/status

# Verify hierarchy configuration
grep -A 10 "region_hierarchy" /etc/moonlight/mls.json
```

### Performance Tuning

#### For High Throughput
- Increase `max_message_size` for large payloads
- Tune keepalive settings for network conditions  
- Use connection pooling on client side
- Enable compression for bandwidth-limited networks

#### For Mobile Networks
- Reduce keepalive intervals
- Enable aggressive reconnection
- Use smaller message sizes
- Implement client-side caching

### Contributing

1. Fork the repository
2. Create feature branch: `git checkout -b feature-name`
3. Make changes and test: `make test`
4. Generate protobuf code: `make proto`
5. Submit pull request

### License

MIT License - see LICENSE file for details.