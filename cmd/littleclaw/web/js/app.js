/**
 * Littleclaw Face UI - Main Application
 * Manages WebSocket connection, state updates, and UI coordination.
 */
class LittleclawUI {
    constructor() {
        this.face = new FaceController();
        this.particles = new ParticleEngine('particles-canvas');
        this.ws = null;
        this.reconnectInterval = 2000;
        this.maxTimelineEntries = 50;

        // DOM references
        this.connectionDot = document.getElementById('connection-dot');
        this.connectionStatus = document.getElementById('connection-status');
        this.uptimeEl = document.getElementById('uptime');
        this.cronCountEl = document.getElementById('cron-count');
        this.entityCountEl = document.getElementById('entity-count');
        this.timelineEl = document.getElementById('timeline');
        this.lastUserMsgEl = document.getElementById('last-user-msg');
        this.lastBotMsgEl = document.getElementById('last-bot-msg');

        this.connect();
    }

    /**
     * Connect to WebSocket
     */
    connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws`;

        this.ws = new WebSocket(wsUrl);

        this.ws.onopen = () => {
            this.setConnected(true);
            console.log('[Littleclaw] WebSocket connected');
        };

        this.ws.onclose = () => {
            this.setConnected(false);
            console.log('[Littleclaw] WebSocket disconnected, reconnecting...');
            setTimeout(() => this.connect(), this.reconnectInterval);
        };

        this.ws.onerror = (err) => {
            console.error('[Littleclaw] WebSocket error:', err);
        };

        this.ws.onmessage = (event) => {
            try {
                const msg = JSON.parse(event.data);
                this.handleMessage(msg);
            } catch (e) {
                console.error('[Littleclaw] Failed to parse message:', e);
            }
        };
    }

    /**
     * Handle incoming WebSocket message
     */
    handleMessage(msg) {
        switch (msg.type) {
            case 'state':
                this.handleState(msg.data);
                break;
            case 'activity':
                this.addTimelineEntry(msg.data);
                break;
            case 'stats':
                this.handleStats(msg.data);
                break;
            case 'history':
                this.handleHistory(msg.data);
                break;
        }
    }

    /**
     * Handle state update
     */
    handleState(state) {
        // Update face expression
        this.face.updateFromState(state);

        // Update particle effects
        const config = EXPRESSIONS[state.expression] || EXPRESSIONS.idle;
        this.particles.setType(config.particleType, config.glowColor);

        // Update last messages
        if (state.last_user_message) {
            this.lastUserMsgEl.textContent = state.last_user_message;
        }
        if (state.last_bot_message) {
            this.lastBotMsgEl.textContent = state.last_bot_message;
        }

        // Update uptime
        if (state.uptime_seconds !== undefined) {
            this.uptimeEl.textContent = this.formatUptime(state.uptime_seconds);
        }
    }

    /**
     * Handle stats update
     */
    handleStats(stats) {
        if (stats.active_cron_jobs !== undefined) {
            this.cronCountEl.textContent = stats.active_cron_jobs;
        }
        if (stats.entity_count !== undefined) {
            this.entityCountEl.textContent = stats.entity_count;
        }
        if (stats.uptime_seconds !== undefined) {
            this.uptimeEl.textContent = this.formatUptime(stats.uptime_seconds);
        }
    }

    /**
     * Handle initial history load
     */
    handleHistory(entries) {
        if (!Array.isArray(entries)) return;

        // Clear "waiting" placeholder
        this.timelineEl.innerHTML = '';

        entries.forEach(entry => this.addTimelineEntry(entry, false));

        // Scroll to bottom
        this.timelineEl.scrollTop = this.timelineEl.scrollHeight;
    }

    /**
     * Add an entry to the activity timeline
     */
    addTimelineEntry(entry, scrollToBottom = true) {
        if (!entry || !entry.kind) return;

        // Remove placeholder if present
        const placeholder = this.timelineEl.querySelector('.timeline-empty');
        if (placeholder) placeholder.remove();

        const kind = ACTIVITY_KINDS[entry.kind] || { color: '#888', label: entry.kind };
        const time = entry.timestamp ? new Date(entry.timestamp).toLocaleTimeString('en-US', {
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
            hour12: false
        }) : '';

        const el = document.createElement('div');
        el.className = 'timeline-entry';
        el.innerHTML = `
            <span class="timeline-time">${time}</span>
            <span class="timeline-dot" style="background:${kind.color}"></span>
            <div class="timeline-content">
                <span class="timeline-title">${this.escapeHtml(entry.title || kind.label)}</span>
                ${entry.detail ? `<div class="timeline-detail">${this.escapeHtml(entry.detail)}</div>` : ''}
            </div>
        `;

        this.timelineEl.appendChild(el);

        // Trim old entries
        while (this.timelineEl.children.length > this.maxTimelineEntries) {
            this.timelineEl.removeChild(this.timelineEl.firstChild);
        }

        // Auto-scroll
        if (scrollToBottom) {
            this.timelineEl.scrollTop = this.timelineEl.scrollHeight;
        }
    }

    /**
     * Set connection status
     */
    setConnected(connected) {
        if (connected) {
            this.connectionDot.classList.add('connected');
            this.connectionStatus.textContent = 'Connected';
        } else {
            this.connectionDot.classList.remove('connected');
            this.connectionStatus.textContent = 'Disconnected';
        }
    }

    /**
     * Format uptime in seconds to human-readable string
     */
    formatUptime(seconds) {
        if (seconds < 60) return `${seconds}s`;
        if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
        const h = Math.floor(seconds / 3600);
        const m = Math.floor((seconds % 3600) / 60);
        return `${h}h ${m}m`;
    }

    /**
     * Escape HTML to prevent XSS
     */
    escapeHtml(str) {
        if (!str) return '';
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }
}

// Boot the app
document.addEventListener('DOMContentLoaded', () => {
    window.littleclaw = new LittleclawUI();
});
