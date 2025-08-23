#!/usr/bin/env node

const WebSocket = require('ws');

class RobustWebSocketClient {
    constructor(serverUrl, config) {
        this.serverUrl = serverUrl;
        this.config = config;
        this.ws = null;
        this.connected = false;
        this.reconnectAttempts = 0;
        this.maxReconnectAttempts = 10;
        this.reconnectDelay = 5000; // 5 seconds
        this.heartbeatInterval = 30000; // 30 seconds
        this.heartbeatTimer = null;
        this.registrationTimer = null;
        this.isRegistered = false;
    }

    connect() {
        console.log(`Connecting to ${this.serverUrl}...`);

        try {
            this.ws = new WebSocket(this.serverUrl);
            this.setupEventHandlers();
        } catch (error) {
            console.error('Failed to create WebSocket connection:', error);
            this.scheduleReconnect();
        }
    }

    setupEventHandlers() {
        this.ws.on('open', () => {
            console.log('WebSocket connection established');
            this.connected = true;
            this.reconnectAttempts = 0;

            // Register immediately
            this.register();
        });

        this.ws.on('message', (data) => {
            try {
                const message = JSON.parse(data);
                this.handleMessage(message);
            } catch (error) {
                console.error('Failed to parse message:', error);
            }
        });

        this.ws.on('close', (code, reason) => {
            console.log(`WebSocket connection closed (code: ${code}, reason: ${reason})`);
            this.connected = false;
            this.isRegistered = false;
            this.stopHeartbeat();
            this.stopRegistrationTimer();

            // Only reconnect if it wasn't a clean close
            if (code !== 1000) {
                this.scheduleReconnect();
            }
        });

        this.ws.on('error', (error) => {
            console.error('WebSocket error:', error);
            this.connected = false;
        });

        this.ws.on('ping', () => {
            console.log('Received ping, sending pong');
            this.ws.pong();
        });

        this.ws.on('pong', () => {
            console.log('Received pong');
        });
    }

    register() {
        if (!this.connected) {
            console.log('Cannot register: not connected');
            return;
        }

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

        // Set up registration timeout
        this.registrationTimer = setTimeout(() => {
            if (!this.isRegistered) {
                console.log('Registration timeout, reconnecting...');
                this.ws.close();
            }
        }, 10000); // 10 second timeout
    }

    startHeartbeat() {
        this.stopHeartbeat(); // Clear any existing timer

        this.heartbeatTimer = setInterval(() => {
            if (this.connected && this.isRegistered) {
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

    stopRegistrationTimer() {
        if (this.registrationTimer) {
            clearTimeout(this.registrationTimer);
            this.registrationTimer = null;
        }
    }

    handleMessage(message) {
        console.log('Received message:', message.type);

        switch (message.type) {
            case 'registered':
                console.log('Successfully registered with server');
                this.isRegistered = true;
                this.stopRegistrationTimer();
                this.startHeartbeat();
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
        console.log(`Processing task ${message.task_id}:`, message.payload);

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
                    response: JSON.stringify({ error: error.message }),
                    status: 500,
                    headers: { 'Content-Type': 'application/json' }
                }
            };

            this.send(errorResponse);
        }
    }

    async processTask(payload) {
        // This is where you implement your actual task processing logic
        const taskData = JSON.parse(payload);

        // Simulate some processing time
        await new Promise(resolve => setTimeout(resolve, 100));

        return {
            data: JSON.stringify({
                processed: true,
                original: taskData,
                timestamp: new Date().toISOString()
            }),
            status: 200,
            headers: { 'Content-Type': 'application/json' }
        };
    }

    send(message) {
        if (this.connected && this.ws && this.ws.readyState === WebSocket.OPEN) {
            try {
                this.ws.send(JSON.stringify(message));
            } catch (error) {
                console.error('Failed to send message:', error);
                this.connected = false;
            }
        } else {
            console.error('Cannot send message: not connected or connection not ready');
        }
    }

    scheduleReconnect() {
        if (this.reconnectAttempts >= this.maxReconnectAttempts) {
            console.error('Max reconnection attempts reached, giving up');
            return;
        }

        this.reconnectAttempts++;
        const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1); // Exponential backoff

        console.log(`Scheduling reconnection attempt ${this.reconnectAttempts} in ${delay}ms`);

        setTimeout(() => {
            console.log(`Attempting reconnection ${this.reconnectAttempts}/${this.maxReconnectAttempts}`);
            this.connect();
        }, delay);
    }

    disconnect() {
        console.log('Disconnecting...');
        this.stopHeartbeat();
        this.stopRegistrationTimer();
        this.connected = false;
        this.isRegistered = false;

        if (this.ws) {
            this.ws.close(1000, 'Client disconnect');
        }
    }

    // Get connection status
    getStatus() {
        return {
            connected: this.connected,
            registered: this.isRegistered,
            reconnectAttempts: this.reconnectAttempts,
            readyState: this.ws ? this.ws.readyState : null
        };
    }
}

// Example usage
if (require.main === module) {
    const config = {
        ip: '192.168.1.100',
        nodeId: 'robust-node-001',
        token: 'supersecrettokenox0',
        region: 'us-west',
        port: 3000
    };

    const client = new RobustWebSocketClient('ws://localhost:8080/ws', config);
    client.connect();

    // Handle graceful shutdown
    process.on('SIGINT', () => {
        console.log('Shutting down...');
        client.disconnect();
        process.exit(0);
    });

    // Log status every 30 seconds
    setInterval(() => {
        const status = client.getStatus();
        console.log('Client status:', status);
    }, 30000);
}

module.exports = RobustWebSocketClient;
