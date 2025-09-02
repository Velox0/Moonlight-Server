let memoryChart, requestsChart, clientsChart;

// Initialize charts on DOM ready
function initMonitorCharts() {
    const memoryCtx = document.getElementById('memoryChart').getContext('2d');
    memoryChart = new Chart(memoryCtx, {
        type: 'line',
        data: {
            labels: [],
            datasets: [
                { label: 'Memory Alloc (MB)', borderColor: 'rgba(75,192,192,1)', data: [], fill: false },
                { label: 'Total Alloc (MB)', borderColor: 'rgba(153,102,255,1)', data: [], fill: false }
            ]
        },
        options: {
            responsive: true,
            scales: { x: { display: true }, y: { beginAtZero: true } }
        }
    });

    const clientsCtx = document.getElementById('clientsChart').getContext('2d');
    clientsChart = new Chart(clientsCtx, {
        type: 'line',
        data: {
            labels: [],
            datasets: [{
                label: 'Connected Clients',
                borderColor: 'rgba(54, 162, 235, 1)',
                data: [],
                fill: false
            }]
        },
        options: {
            responsive: true,
            scales: { x: { display: true }, y: { beginAtZero: true } }
        }
    });
}

async function fetchMonitorData() {
    try {
        const resp = await fetch('/monitor');
        if (!resp.ok) throw new Error('Network error');
        const data = await resp.json();
        updateCharts(data);
    } catch (e) {
        console.error('Error fetching /monitor:', e);
    }
}

function updateCharts(data) {
    const nowLabel = new Date().toLocaleTimeString();

    // Memory Chart (convert bytes to MB)
    memoryChart.data.labels.push(nowLabel);
    memoryChart.data.datasets[0].data.push((data.memory_alloc_bytes / 1024 / 1024).toFixed(2));
    memoryChart.data.datasets[1].data.push((data.total_alloc_bytes / 1024 / 1024).toFixed(2));
    if (memoryChart.data.labels.length > 20) {
        memoryChart.data.labels.shift();
        memoryChart.data.datasets[0].data.shift();
        memoryChart.data.datasets[1].data.shift();
    }
    memoryChart.update();

    // Clients Chart
    clientsChart.data.labels.push(nowLabel);
    clientsChart.data.datasets[0].data.push(data.client_count);
    if (clientsChart.data.labels.length > 20) {
        clientsChart.data.labels.shift();
        clientsChart.data.datasets[0].data.shift();
    }
    clientsChart.update();
}