#!/usr/bin/env node

// Simple Node.js client for Moonlight Server
// - Sends periodic heartbeats to register itself
// - Exposes /work to echo back request body;

// TODO:
// - An npm package for simplifying the client setup

const http = require('http');
const os = require('os');

const SERVER_HOST = process.env.MLS_HOST || 'localhost';
const SERVER_PORT = Number(process.env.MLS_PORT || 8080);
const TOKEN = process.env.MLS_TOKEN || 'supersecrettokenox0';
const REGION = process.env.MLS_REGION || 'us-east-1';
const NODE_ID = process.env.MLS_NODE_ID || os.hostname();
const CLIENT_PORT = Number(process.env.CLIENT_PORT || 3000);

function getLocalIPv4() {
    const interfaces = os.networkInterfaces();
    for (const name of Object.keys(interfaces)) {
        for (const iface of interfaces[name] || []) {
            if (iface.family === 'IPv4' && !iface.internal) {
                return iface.address;
            }
        }
    }
    return '127.0.0.1';
}

const CLIENT_IP = process.env.MLS_CLIENT_IP || getLocalIPv4();

function postJSON(path, data) {
    return new Promise((resolve, reject) => {
        const req = http.request(
            {
                host: SERVER_HOST,
                port: SERVER_PORT,
                path,
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
            },
            (res) => {
                let body = '';
                res.on('data', (chunk) => (body += chunk));
                res.on('end', () => resolve({ status: res.statusCode, body }));
            }
        );
        req.on('error', reject);
        req.write(JSON.stringify(data));
        req.end();
    });
}

async function sendHeartbeat() {
    console.log('Sending heartbeat');
    try {
        const payload = {
            ip: CLIENT_IP,
            node_id: NODE_ID,
            token: TOKEN,
            region: REGION,
            port: CLIENT_PORT,
        };
        console.log('Payload:', [payload.ip, payload.node_id, payload.token, payload.region]);
        const res = await postJSON('/client/heartbeat', payload);
        if (res.status !== 200) {
            console.error('Heartbeat failed:', res.status, res.body);
        }
    } catch (err) {
        console.error('Heartbeat error:', err.message || err);
    }
}

// Start simple HTTP server with /work endpoint that echoes back the raw JSON body
const app = http.createServer(async (req, res) => {
    if (req.method === 'POST' && req.url === '/work') {
        let body = '';
        req.on('data', (chunk) => (body += chunk));
        req.on('end', () => {
            console.log('Received work body:', body);
            try {
                // Validate JSON
                JSON.parse(body || '{}');
                res.setHeader('Content-Type', 'application/json');
                res.end(body);
            } catch (e) {
                res.statusCode = 400;
                res.setHeader('Content-Type', 'application/json');
                res.end(JSON.stringify({ error: 'invalid json' }));
            }
        });
        return;
    }
    res.statusCode = 404;
    res.end('Not Found');
});

app.listen(CLIENT_PORT, () => {
    console.log(`Node client listening on http://localhost:${CLIENT_PORT}`);
    console.log(`Moonlight server assumed at http://${SERVER_HOST}:${SERVER_PORT}`);
    console.log(`Client IP reported as: ${CLIENT_IP}`);
});

// Initial heartbeat and then every 15 seconds
sendHeartbeat();
setInterval(sendHeartbeat, 15_000);


