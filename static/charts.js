let memoryChart, requestsChart, clientsChart;
let animationFrameId;
let isAnimating = false;
const MAX_DATA_POINTS = 20;
const ANIMATION_DURATION = 200; // 2 seconds for smooth left shift

// Store pending data to add after animation completes
let pendingData = null;

// Initialize charts on DOM ready
function initMonitorCharts() {
    // Get canvas elements and set safe dimensions
    const memoryCanvas = document.getElementById('memoryChart');
    const clientsCanvas = document.getElementById('clientsChart');

    if (!memoryCanvas || !clientsCanvas) {
        console.error('Canvas elements not found');
        return;
    }

    // Set fixed, safe canvas dimensions
    memoryCanvas.width = 600;
    memoryCanvas.height = 200;
    clientsCanvas.width = 600;
    clientsCanvas.height = 200;

    // Initialize with full array of zeros and time labels
    const initialLabels = [];
    const initialMemoryData = [];
    const initialTotalData = [];
    const initialClientsData = [];

    const now = new Date();
    for (let i = MAX_DATA_POINTS - 1; i >= 0; i--) {
        const time = new Date(now.getTime() - (i * 2000)); // 2 second intervals
        initialLabels.push(time.toLocaleTimeString('en-US', {
            hour12: false,
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit'
        }));
        initialMemoryData.push(0);
        initialTotalData.push(0);
        initialClientsData.push(0);
    }

    // Enhanced chart configuration with fixed sizing
    const chartOptions = {
        responsive: false, // Disable responsive to use fixed canvas size
        maintainAspectRatio: true,
        devicePixelRatio: 1, // Force pixel ratio to 1 to prevent scaling issues
        plugins: {
            legend: {
                position: 'top',
                labels: {
                    usePointStyle: true,
                    padding: 15,
                    font: {
                        size: 11,
                        weight: '500'
                    }
                }
            },
            tooltip: {
                backgroundColor: 'rgba(0, 0, 0, 0.8)',
                titleColor: 'white',
                bodyColor: 'white',
                borderColor: 'rgba(255, 255, 255, 0.1)',
                borderWidth: 1,
                cornerRadius: 6,
                displayColors: true,
                mode: 'index',
                intersect: false
            }
        },
        scales: {
            x: {
                display: true,
                grid: {
                    color: 'rgba(0, 0, 0, 0.1)',
                    drawBorder: false
                },
                ticks: {
                    font: { size: 10 },
                    color: '#64748b',
                    maxRotation: 0
                }
            },
            y: {
                beginAtZero: true,
                grid: {
                    color: 'rgba(0, 0, 0, 0.1)',
                    drawBorder: false
                },
                ticks: {
                    font: { size: 10 },
                    color: '#64748b'
                }
            }
        },
        interaction: {
            intersect: false,
            mode: 'index'
        },
        elements: {
            point: {
                radius: 3,
                hoverRadius: 5,
                borderWidth: 2
            },
            line: {
                borderWidth: 2,
                tension: 0.3
            }
        },
        animation: {
            duration: 0, // Disable default animations since we'll handle custom ones
            easing: 'linear'
        }
    };

    // Memory Chart with enhanced styling
    const memoryCtx = memoryCanvas.getContext('2d');
    memoryChart = new Chart(memoryCtx, {
        type: 'line',
        data: {
            labels: [...initialLabels],
            datasets: [
                {
                    label: 'Memory Alloc (MB)',
                    borderColor: 'rgba(59, 130, 246, 1)',
                    backgroundColor: 'rgba(59, 130, 246, 0.1)',
                    data: [...initialMemoryData],
                    fill: true,
                    pointBackgroundColor: 'rgba(59, 130, 246, 1)',
                    pointBorderColor: '#ffffff',
                    pointBorderWidth: 1
                },
                {
                    label: 'Total Alloc (MB)',
                    borderColor: 'rgba(147, 51, 234, 1)',
                    backgroundColor: 'rgba(147, 51, 234, 0.1)',
                    data: [...initialTotalData],
                    fill: true,
                    pointBackgroundColor: 'rgba(147, 51, 234, 1)',
                    pointBorderColor: '#ffffff',
                    pointBorderWidth: 1
                }
            ]
        },
        options: chartOptions
    });

    // Clients Chart with enhanced styling
    const clientsCtx = clientsCanvas.getContext('2d');
    clientsChart = new Chart(clientsCtx, {
        type: 'line',
        data: {
            labels: [...initialLabels],
            datasets: [{
                label: 'Connected Clients',
                borderColor: 'rgba(16, 185, 129, 1)',
                backgroundColor: 'rgba(16, 185, 129, 0.1)',
                data: [...initialClientsData],
                fill: true,
                pointBackgroundColor: 'rgba(16, 185, 129, 1)',
                pointBorderColor: '#ffffff',
                pointBorderWidth: 1
            }]
        },
        options: chartOptions
    });

    console.log('Charts initialized successfully');
}

async function fetchMonitorData() {
    try {
        const resp = await fetch('/monitor');
        if (!resp.ok) throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);
        const data = await resp.json();

        if (isAnimating) {
            // Store data to be processed after animation completes
            pendingData = data;
        } else {
            updateCharts(data);
        }
    } catch (e) {
        console.error('Error fetching /monitor:', e);
    }
}

function updateCharts(data) {
    if (isAnimating) return; // Don't update while animating

    isAnimating = true;

    const nowLabel = new Date().toLocaleTimeString('en-US', {
        hour12: false,
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
    });

    // Memory Chart (convert bytes to MB)
    const memoryAllocMB = parseFloat((data.memory_alloc_bytes / 1024 / 1024).toFixed(2));
    const totalAllocMB = parseFloat((data.total_alloc_bytes / 1024 / 1024).toFixed(2));

    // Store original chart data for animation
    const originalMemoryData = {
        labels: [...memoryChart.data.labels],
        datasets: [
            { data: [...memoryChart.data.datasets[0].data] },
            { data: [...memoryChart.data.datasets[1].data] }
        ]
    };

    const originalClientsData = {
        labels: [...clientsChart.data.labels],
        datasets: [{ data: [...clientsChart.data.datasets[0].data] }]
    };

    // Prepare final data (shift left and add new point)
    const finalMemoryData = {
        labels: [...memoryChart.data.labels.slice(1), nowLabel],
        datasets: [
            { data: [...memoryChart.data.datasets[0].data.slice(1), memoryAllocMB] },
            { data: [...memoryChart.data.datasets[1].data.slice(1), totalAllocMB] }
        ]
    };

    const finalClientsData = {
        labels: [...clientsChart.data.labels.slice(1), nowLabel],
        datasets: [{ data: [...clientsChart.data.datasets[0].data.slice(1), data.client_count] }]
    };

    // Start sliding animation
    animateSlide(originalMemoryData, finalMemoryData, originalClientsData, finalClientsData);
}

function animateSlide(originalMemory, finalMemory, originalClients, finalClients) {
    const startTime = Date.now();
    const duration = ANIMATION_DURATION;

    function animate() {
        const elapsed = Date.now() - startTime;
        const progress = Math.min(elapsed / duration, 1);

        // Use easeOutCubic for smooth deceleration
        const easedProgress = 1 - Math.pow(1 - progress, 3);

        // Interpolate between original and final data
        interpolateChartData(memoryChart, originalMemory, finalMemory, easedProgress);
        interpolateChartData(clientsChart, originalClients, finalClients, easedProgress);

        // Update charts
        memoryChart.update('none');
        clientsChart.update('none');

        if (progress < 1) {
            animationFrameId = requestAnimationFrame(animate);
        } else {
            // Animation complete
            isAnimating = false;

            // Process any pending data
            if (pendingData) {
                const data = pendingData;
                pendingData = null;
                setTimeout(() => updateCharts(data), 100); // Small delay before next animation
            }
        }
    }

    animate();
}

function interpolateChartData(chart, originalData, finalData, progress) {
    // Interpolate labels (no interpolation needed, just switch at progress > 0.5)
    if (progress > 0.5) {
        chart.data.labels = [...finalData.labels];
    } else {
        chart.data.labels = [...originalData.labels];
    }

    // Interpolate data values
    for (let i = 0; i < chart.data.datasets.length; i++) {
        const dataset = chart.data.datasets[i];
        const originalValues = originalData.datasets[i].data;
        const finalValues = finalData.datasets[i].data;

        dataset.data = originalValues.map((originalValue, index) => {
            const finalValue = finalValues[index] || 0;
            return originalValue + (finalValue - originalValue) * progress;
        });
    }
}

// Enhanced error handling for network issues
function startMonitoring() {
    // Initial fetch
    fetchMonitorData();

    // Set up periodic updates every 10 seconds
    setInterval(fetchMonitorData, 10000);
}

// Set global Chart.js defaults to prevent canvas size issues
Chart.defaults.devicePixelRatio = 1;
Chart.defaults.animation.duration = 0; // Disable default animations
Chart.defaults.animation.easing = 'linear';