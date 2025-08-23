const WebSocket = require('ws');

class MoonlightWebSocketClient {
    constructor(serverUrl, config) {
        this.serverUrl = serverUrl;
        this.config = config;
        this.ws = null;
        this.connected = false;
        this.reconnectInterval = 5000; // 5 seconds
        this.heartbeatInterval = 30000; // 30 seconds
        this.heartbeatTimer = null;
    }

    connect() {
        console.log(`Connecting to ${this.serverUrl}...`);

        this.ws = new WebSocket(this.serverUrl);

        this.ws.on('open', () => {
            console.log('WebSocket connection established');
            this.connected = true;

            // Register client
            this.register();

            // Start heartbeat
            this.startHeartbeat();
        });

        this.ws.on('message', (data) => {
            try {
                const message = JSON.parse(data);
                this.handleMessage(message);
            } catch (error) {
                console.error('Failed to parse message:', error);
            }
        });

        this.ws.on('close', () => {
            console.log('WebSocket connection closed');
            this.connected = false;
            this.stopHeartbeat();

            // Attempt to reconnect
            setTimeout(() => {
                this.connect();
            }, this.reconnectInterval);
        });

        this.ws.on('error', (error) => {
            console.error('WebSocket error:', error);
        });
    }

    register() {
        const registerMessage = {
            type: 'register',
            payload: {
                ip: this.config.ip,
                node_id: this.config.nodeId,
                token: this.config.token,
                region: this.config.region,
                port: this.config.port || 3000
            }
        };

        this.send(registerMessage);
    }

    startHeartbeat() {
        this.heartbeatTimer = setInterval(() => {
            if (this.connected) {
                const heartbeatMessage = {
                    type: 'heartbeat',
                    payload: {
                        timestamp: new Date().toISOString()
                    }
                };
                this.send(heartbeatMessage);
            }
        }, this.heartbeatInterval);
    }

    stopHeartbeat() {
        if (this.heartbeatTimer) {
            clearInterval(this.heartbeatTimer);
            this.heartbeatTimer = null;
        }
    }

    handleMessage(message) {
        switch (message.type) {
            case 'registered':
                console.log('Successfully registered with server');
                break;

            case 'heartbeat_ack':
                console.log('Heartbeat acknowledged');
                break;

            case 'task':
                this.handleTask(message);
                break;

            case 'error':
                console.error('Server error:', message.payload);
                break;

            default:
                console.log('Unknown message type:', message.type);
        }
    }

    async handleTask(message) {
        console.log(`Received task ${message.task_id}:`, message.payload);

        try {
            // Process the task (this is where your actual work happens)
            const result = await this.processTask(message.payload);

            // Send task response
            const responseMessage = {
                type: 'task_response',
                payload: {
                    task_id: message.task_id,
                    response: result.data,
                    status: result.status,
                    headers: result.headers || {}
                }
            };

            this.send(responseMessage);
            console.log(`Task ${message.task_id} completed successfully`);

        } catch (error) {
            console.error(`Task ${message.task_id} failed:`, error);

            // Send error response
            const errorResponse = {
                type: 'task_response',
                payload: {
                    task_id: message.task_id,
                    response: { error: error.message },
                    status: 500,
                    headers: { 'Content-Type': 'application/json' }
                }
            };

            this.send(errorResponse);
        }
    }

    async processTask(payload) {
        // This is where you implement your actual task processing logic
        // For this example, we'll just echo back the payload
        console.log("Type: ", typeof payload);
        console.log("Payload: ", payload);

        // Simulate some processing time
        await new Promise(resolve => setTimeout(resolve, 100));

        // Return raw data instead of pre-serialized JSON to avoid double serialization
        return {
            data: {
                processed: true,
                timestamp: new Date().toISOString(),
                data: payload
            },
            status: 200,
            headers: { 'Content-Type': 'application/json' }
        };
    }

    send(message) {
        if (this.connected && this.ws) {
            this.ws.send(JSON.stringify(message));
        } else {
            console.error('Cannot send message: not connected');
        }
    }

    disconnect() {
        this.stopHeartbeat();
        if (this.ws) {
            this.ws.close();
        }
    }
}

// Example usage
if (require.main === module) {
    const config = {
        ip: '192.168.1.100',
        nodeId: 'node-002',
        token: 'supersecrettokenox0',
        region: 'us-west',
        port: 3000
    };

    const client = new MoonlightWebSocketClient('ws://localhost:8080/ws', config);
    client.connect();

    // Handle graceful shutdown
    process.on('SIGINT', () => {
        console.log('Shutting down...');
        client.disconnect();
        process.exit(0);
    });
}

module.exports = MoonlightWebSocketClient;
