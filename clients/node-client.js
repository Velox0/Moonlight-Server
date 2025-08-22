// grpc-client.js - Sample gRPC client for Moonlight Server
const grpc = require('@grpc/grpc-js');
const protoLoader = require('@grpc/proto-loader');
const express = require('express');

// Load the protobuf
const PROTO_PATH = './moonlight.proto';
const packageDefinition = protoLoader.loadSync(PROTO_PATH, {
    keepCase: true,
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true
});

const moonlight = grpc.loadPackageDefinition(packageDefinition).moonlight;

class MoonlightGRPCClient {
    constructor(serverAddr, clientConfig) {
        this.serverAddr = serverAddr;
        this.config = clientConfig;
        this.client = new moonlight.Moonlight(serverAddr, grpc.credentials.createInsecure());
        this.stream = null;
        this.isConnected = false;
        this.reconnectAttempts = 0;
        this.maxReconnectAttempts = 10;
        this.reconnectDelay = 5000; // 5 seconds

        // Task processing
        this.pendingTasks = new Map();

        // Start HTTP server for local work processing (optional, for compatibility)
        this.startLocalHTTPServer();
    }

    connect() {
        console.log(`Connecting to gRPC server at ${this.serverAddr}...`);

        this.stream = this.client.Connect();

        // Handle connection events
        this.stream.on('data', (message) => {
            this.handleServerMessage(message);
        });

        this.stream.on('end', () => {
            console.log('Server ended the connection');
            this.isConnected = false;
            this.scheduleReconnect();
        });

        this.stream.on('error', (error) => {
            console.error('Stream error:', error.message);
            this.isConnected = false;
            this.scheduleReconnect();
        });

        // Send initial hello message
        this.sendHello();
    }

    sendHello() {
        const hello = {
            hello: {
                ip: this.config.ip,
                node_id: this.config.nodeId,
                token: this.config.token,
                region: this.config.region,
                port: this.config.port || 3000
            }
        };

        console.log('Sending hello:', hello.hello);
        this.stream.write(hello);
        this.isConnected = true;
        this.reconnectAttempts = 0;
        console.log('Connected to Moonlight server via gRPC');
    }

    handleServerMessage(message) {
        if (message.task) {
            this.handleTask(message.task);
        } else if (message.hello) {
            console.log('Received hello from server:', message.hello);
        } else {
            console.log('Unknown message type:', message);
        }
    }

    async handleTask(task) {
        console.log(`Received task ${task.id}:`, JSON.parse(task.payload.toString()));

        try {
            // Process the task - customize this for your use case
            const result = await this.processTask(JSON.parse(task.payload.toString()));

            // Send result back to server
            this.sendResult(task.id, result, null);

        } catch (error) {
            console.error(`Task ${task.id} failed:`, error.message);
            this.sendResult(task.id, null, error.message);
        }
    }

    async processTask(payload) {
        // Simulate work - customize this for your actual task processing
        console.log('Processing task:', payload);

        // Example: echo the payload with some processing
        const result = {
            processed_at: new Date().toISOString(),
            original_payload: payload,
            result: `Processed by gRPC client ${this.config.nodeId}`,
            client_info: {
                node_id: this.config.nodeId,
                region: this.config.region,
                connection_type: 'grpc'
            }
        };

        // Simulate some async work
        await new Promise(resolve => setTimeout(resolve, Math.random() * 1000));

        return result;
    }

    sendResult(taskId, data, error) {
        const result = {
            result: {
                id: taskId,
                data: Buffer.from(JSON.stringify(data || {})),
                err: error || ''
            }
        };

        if (this.isConnected && this.stream) {
            this.stream.write(result);
            console.log(`Sent result for task ${taskId}`);
        } else {
            console.error(`Cannot send result for task ${taskId}: not connected`);
        }
    }

    scheduleReconnect() {
        if (this.reconnectAttempts >= this.maxReconnectAttempts) {
            console.error('Max reconnection attempts reached. Giving up.');
            return;
        }

        this.reconnectAttempts++;
        const delay = this.reconnectDelay * Math.pow(2, Math.min(this.reconnectAttempts - 1, 5)); // Exponential backoff

        console.log(`Scheduling reconnect attempt ${this.reconnectAttempts} in ${delay}ms`);
        setTimeout(() => {
            console.log(`Reconnect attempt ${this.reconnectAttempts}...`);
            this.connect();
        }, delay);
    }

    // Optional: Start a local HTTP server for compatibility with HTTP-based tools
    startLocalHTTPServer() {
        const app = express();
        app.use(express.json());

        app.post('/work', async (req, res) => {
            try {
                const result = await this.processTask(req.body);
                res.json(result);
            } catch (error) {
                res.status(500).json({ error: error.message });
            }
        });

        app.get('/status', (req, res) => {
            res.json({
                connected: this.isConnected,
                node_id: this.config.nodeId,
                region: this.config.region,
                connection_type: 'grpc',
                reconnect_attempts: this.reconnectAttempts
            });
        });

        const port = this.config.port || 3000;
        app.listen(port, () => {
            console.log(`HTTP compatibility server listening on port ${port}`);
        });
    }

    disconnect() {
        console.log('Disconnecting from server...');
        this.isConnected = false;
        if (this.stream) {
            this.stream.end();
        }
    }
}

// Example usage
if (require.main === module) {
    const client = new MoonlightGRPCClient('localhost:8081', {
        ip: '192.168.1.100', // Your client's IP
        nodeId: 'mobile-client-001',
        token: 'supersecrettokenox0',
        region: 'us-east-1',
        port: 3000
    });

    // Connect to server
    client.connect();

    // Handle graceful shutdown
    process.on('SIGINT', () => {
        console.log('Shutting down gracefully...');
        client.disconnect();
        process.exit(0);
    });

    process.on('SIGTERM', () => {
        console.log('Received SIGTERM, shutting down...');
        client.disconnect();
        process.exit(0);
    });
}

module.exports = MoonlightGRPCClient;