let ws = null;

// Theme management
let currentTheme = localStorage.getItem('theme') || 'dark';

function setTheme(theme) {
    console.log('setTheme called with:', theme);

    document.documentElement.removeAttribute('data-theme');
    setTimeout(() => {
        document.documentElement.setAttribute('data-theme', theme);
        console.log('data-theme attribute set to:', document.documentElement.getAttribute('data-theme'));
    }, 10);

    currentTheme = theme;
    localStorage.setItem('theme', theme);

    const themeToggle = document.getElementById('theme-toggle');
    const themeIcon = themeToggle.querySelector('.theme-icon');

    if (theme === 'light') {
        themeIcon.textContent = '☀️';
    } else {
        themeIcon.textContent = '🌙';
    }

    // Update debug indicator
    const themeDebug = document.getElementById('theme-debug');
    if (themeDebug) {
        themeDebug.textContent = `Theme: ${theme}`;
        themeDebug.style.background = theme === 'light' ? 'black' : 'red';
    }

    console.log('Theme set successfully to:', theme);
}

function toggleTheme() {
    console.log('toggleTheme called, currentTheme:', currentTheme);
    const newTheme = currentTheme === 'dark' ? 'light' : 'dark';
    console.log('Switching to theme:', newTheme);
    setTheme(newTheme);
}

function updateWebSocketStatus(text, online) {
    const wsStatusEl = document.getElementById('ws-status');
    wsStatusEl.textContent = text;
}

function initWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws`;

    ws = new WebSocket(wsUrl);

    ws.onopen = function () {
        updateWebSocketStatus('Connected', true);
        console.log('WebSocket connected');
    };
    ws.onclose = function () {
        updateWebSocketStatus('Disconnected', false);
        console.log('WebSocket disconnected');
        // Reconnect
        setTimeout(initWebSocket, 5000);
    };
    ws.onerror = function (err) {
        updateWebSocketStatus('Error', false);
        console.error('WebSocket error:', err);
    };
}

async function fetchRegions() {
    try {
        const response = await fetch('/region');
        const data = await response.json();
        const regionSelect = document.getElementById('region');

        // Clear existing options except the first one
        regionSelect.innerHTML = '<option value="global">Global (Any Region)</option>';

        // Add region options
        data.regions.forEach(region => {
            if (region !== 'global') {
                const option = document.createElement('option');
                option.value = region;
                option.textContent = region;
                regionSelect.appendChild(option);
            }
        });
    } catch (e) {
        console.error('Error fetching regions:', e);
    }
}

async function fetchClients() {
    try {
        const response = await fetch('/clients/table', {
            headers: { 'Accept': 'application/json' }
        });
        const data = await response.json();
        const clientList = document.getElementById('client-list');
        if (!data.length) {
            clientList.innerHTML = '<p>No clients connected</p>';
            document.getElementById('client-count').textContent = '0';
            return;
        }
        document.getElementById('client-count').textContent = data.length;

        let html = `
                    <table class="clients-table">
                        <thead>
                            <tr>
                                <th>Status</th>
                                <th>Node ID</th>
                                <th>Protocol</th>
                                <th>Last Seen</th>
                            </tr>
                        </thead>
                        <tbody>`;

        data.forEach(client => {
            const isOnline = client.connected === 'connected';
            const statusDotClass = isOnline ? 'online' : 'offline';
            const protocolClass = client.protocol.toLowerCase();

            // Format date nicely
            const date = new Date(client.last_seen);
            const now = new Date();
            const diffMs = now - date;
            const diffMins = Math.floor(diffMs / 60000);
            const diffHours = Math.floor(diffMs / 3600000);
            const diffDays = Math.floor(diffMs / 86400000);

            let lastSeen;
            if (diffMins < 1) {
                lastSeen = 'Just now';
            } else if (diffMins < 60) {
                lastSeen = `${diffMins}m ago`;
            } else if (diffHours < 24) {
                lastSeen = `${diffHours}h ago`;
            } else if (diffDays < 7) {
                lastSeen = `${diffDays}d ago`;
            } else {
                lastSeen = date.toLocaleDateString('en-US', {
                    month: 'short',
                    day: 'numeric',
                    hour: '2-digit',
                    minute: '2-digit'
                });
            }

            html += `
                        <tr>
                            <td>
                                <span class="status-dot ${statusDotClass}"></span>
                                ${isOnline ? 'Online' : 'Offline'}
                            </td>
                            <td><strong>${client.node_id}</strong></td>
                            <td><span class="protocol-badge ${protocolClass}">${client.protocol}</span></td>
                            <td>${lastSeen}</td>
                        </tr>`;
        });

        html += '</tbody></table>';
        clientList.innerHTML = html;
    } catch (e) {
        console.error('Error fetching clients:', e);
        document.getElementById('client-list').innerHTML = '<p>Error loading clients</p>';
    }
}

document.getElementById('task-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const region = document.getElementById('region').value;
    const payloadText = document.getElementById('payload').value;
    const taskResult = document.getElementById('task-result');
    try {
        const payload = JSON.parse(payloadText);
        const response = await fetch('/task/request', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ region, payload })
        });
        const text = await response.text();
        taskResult.innerHTML = `<h4>Response (Status: ${response.status}):</h4><pre>${text}</pre>`;
    } catch (error) {
        taskResult.innerHTML = `<h4>Error:</h4><pre>${error.message}</pre>`;
    }
});

document.addEventListener('DOMContentLoaded', () => {
    console.log('DOM loaded, initializing theme system...');

    // Initialize theme
    setTheme(currentTheme);

    // Add theme toggle event listener
    const themeToggle = document.getElementById('theme-toggle');
    // console.log('Theme toggle button found:', themeToggle);

    if (themeToggle)
        themeToggle.addEventListener('mousedown', toggleTheme);


    initWebSocket();
    document.getElementById('server-status').textContent = 'Online';
    fetchRegions();
    fetchClients();
    setInterval(fetchClients, 3000);
    setInterval(fetchRegions, 30000); // Refresh regions every 30 seconds
});