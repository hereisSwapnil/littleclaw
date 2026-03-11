/**
 * Littleclaw Face - Canvas Particle Effects
 * Renders ambient particles and expression-specific effects.
 */
class ParticleEngine {
    constructor(canvasId) {
        this.canvas = document.getElementById(canvasId);
        this.ctx = this.canvas.getContext('2d');
        this.particles = [];
        this.currentType = 'dust';
        this.animationId = null;
        this.resize();

        window.addEventListener('resize', () => this.resize());
        this.start();
    }

    resize() {
        this.canvas.width = window.innerWidth;
        this.canvas.height = window.innerHeight;
        this.centerX = this.canvas.width * 0.25; // Center of face panel
        this.centerY = this.canvas.height * 0.45;
    }

    /**
     * Switch particle effect type
     */
    setType(type, color) {
        this.currentType = type || 'dust';
        this.accentColor = color || '#6366f1';
    }

    /**
     * Main animation loop
     */
    start() {
        const animate = () => {
            this.ctx.clearRect(0, 0, this.canvas.width, this.canvas.height);

            // Spawn new particles based on type
            this.spawn();

            // Update and draw particles
            for (let i = this.particles.length - 1; i >= 0; i--) {
                const p = this.particles[i];
                this.updateParticle(p);
                this.drawParticle(p);

                if (p.life <= 0 || p.opacity <= 0) {
                    this.particles.splice(i, 1);
                }
            }

            // Cap particle count
            if (this.particles.length > 100) {
                this.particles = this.particles.slice(-100);
            }

            this.animationId = requestAnimationFrame(animate);
        };
        animate();
    }

    /**
     * Spawn particles based on current type
     */
    spawn() {
        const r = Math.random;
        const cx = this.centerX;
        const cy = this.centerY;

        switch (this.currentType) {
            case 'dust':
                if (r() < 0.02) {
                    this.particles.push({
                        x: cx + (r() - 0.5) * 300,
                        y: cy + (r() - 0.5) * 300,
                        vx: (r() - 0.5) * 0.3,
                        vy: -r() * 0.5 - 0.1,
                        size: r() * 2 + 0.5,
                        life: 200 + r() * 200,
                        opacity: r() * 0.3 + 0.1,
                        color: this.accentColor,
                        type: 'circle'
                    });
                }
                break;

            case 'dots':
                if (r() < 0.05) {
                    this.particles.push({
                        x: cx + (r() - 0.5) * 100,
                        y: cy - 20 + r() * 40,
                        vx: (r() - 0.5) * 1,
                        vy: -r() * 1.5,
                        size: r() * 3 + 2,
                        life: 60 + r() * 60,
                        opacity: 0.6,
                        color: this.accentColor,
                        type: 'circle'
                    });
                }
                break;

            case 'text':
                if (r() < 0.08) {
                    this.particles.push({
                        x: cx + 30 + r() * 40,
                        y: cy + (r() - 0.5) * 30,
                        vx: r() * 3 + 1,
                        vy: (r() - 0.5) * 1,
                        size: r() * 2 + 1,
                        life: 40 + r() * 30,
                        opacity: 0.5,
                        color: this.accentColor,
                        type: 'rect'
                    });
                }
                break;

            case 'waves':
                if (r() < 0.03) {
                    this.particles.push({
                        x: cx,
                        y: cy,
                        vx: 0,
                        vy: 0,
                        size: 5,
                        maxSize: 80 + r() * 60,
                        life: 80,
                        opacity: 0.3,
                        color: this.accentColor,
                        type: 'ring'
                    });
                }
                break;

            case 'sparks':
                if (r() < 0.1) {
                    const angle = r() * Math.PI * 2;
                    const speed = r() * 3 + 1;
                    this.particles.push({
                        x: cx + (r() - 0.5) * 60,
                        y: cy + (r() - 0.5) * 60,
                        vx: Math.cos(angle) * speed,
                        vy: Math.sin(angle) * speed,
                        size: r() * 2 + 1,
                        life: 30 + r() * 30,
                        opacity: 0.8,
                        color: this.accentColor,
                        type: 'circle'
                    });
                }
                break;

            case 'orbit':
                if (r() < 0.04) {
                    this.particles.push({
                        x: cx,
                        y: cy,
                        angle: r() * Math.PI * 2,
                        radius: 60 + r() * 40,
                        speed: (r() * 0.02 + 0.01) * (r() > 0.5 ? 1 : -1),
                        size: r() * 3 + 1,
                        life: 150 + r() * 100,
                        opacity: 0.4,
                        color: this.accentColor,
                        type: 'orbit'
                    });
                }
                break;

            case 'float':
                if (r() < 0.03) {
                    this.particles.push({
                        x: cx + (r() - 0.5) * 200,
                        y: cy + 100,
                        vx: (r() - 0.5) * 0.5,
                        vy: -r() * 1.5 - 0.5,
                        size: r() * 4 + 2,
                        life: 120 + r() * 100,
                        opacity: 0.3,
                        color: this.accentColor,
                        type: 'diamond'
                    });
                }
                break;

            case 'stars':
                if (r() < 0.01) {
                    this.particles.push({
                        x: r() * this.canvas.width * 0.5,
                        y: r() * this.canvas.height,
                        vx: 0,
                        vy: 0,
                        size: r() * 2 + 0.5,
                        life: 200 + r() * 300,
                        opacity: 0,
                        maxOpacity: r() * 0.4 + 0.1,
                        color: '#ffffff',
                        type: 'star'
                    });
                }
                break;

            case 'confetti':
                if (r() < 0.15) {
                    const colors = ['#10b981', '#6366f1', '#f59e0b', '#3b82f6', '#f43f5e'];
                    this.particles.push({
                        x: cx + (r() - 0.5) * 200,
                        y: cy - 50,
                        vx: (r() - 0.5) * 4,
                        vy: r() * 2 + 1,
                        size: r() * 4 + 2,
                        life: 80 + r() * 40,
                        opacity: 0.8,
                        rotation: r() * 360,
                        rotSpeed: (r() - 0.5) * 10,
                        color: colors[Math.floor(r() * colors.length)],
                        type: 'rect'
                    });
                }
                break;

            case 'question':
                if (r() < 0.04) {
                    this.particles.push({
                        x: cx + (r() - 0.5) * 150,
                        y: cy + (r() - 0.5) * 150,
                        vx: (r() - 0.5) * 0.5,
                        vy: -r() * 1 - 0.3,
                        size: 10 + r() * 6,
                        life: 80 + r() * 60,
                        opacity: 0.3,
                        color: this.accentColor,
                        type: 'text',
                        char: '?'
                    });
                }
                break;

            case 'neural':
                if (r() < 0.04) {
                    this.particles.push({
                        x: cx + (r() - 0.5) * 200,
                        y: cy + (r() - 0.5) * 200,
                        targetX: cx + (r() - 0.5) * 200,
                        targetY: cy + (r() - 0.5) * 200,
                        size: r() * 3 + 1,
                        life: 100 + r() * 100,
                        opacity: 0.3,
                        color: this.accentColor,
                        type: 'neural'
                    });
                }
                break;
        }
    }

    /**
     * Update a single particle
     */
    updateParticle(p) {
        p.life--;

        switch (p.type) {
            case 'circle':
            case 'rect':
            case 'diamond':
                p.x += p.vx || 0;
                p.y += p.vy || 0;
                p.opacity *= 0.995;
                if (p.rotation !== undefined) {
                    p.rotation += p.rotSpeed || 0;
                }
                break;

            case 'ring':
                p.size += (p.maxSize - p.size) * 0.05;
                p.opacity *= 0.97;
                break;

            case 'orbit':
                p.angle += p.speed;
                p.opacity *= 0.998;
                break;

            case 'star':
                if (p.life > 100) {
                    p.opacity = Math.min(p.opacity + 0.005, p.maxOpacity);
                } else {
                    p.opacity *= 0.99;
                }
                break;

            case 'text':
                p.x += p.vx || 0;
                p.y += p.vy || 0;
                p.opacity *= 0.99;
                break;

            case 'neural':
                p.opacity *= 0.995;
                break;
        }
    }

    /**
     * Draw a single particle
     */
    drawParticle(p) {
        const ctx = this.ctx;
        ctx.globalAlpha = Math.max(0, p.opacity);

        switch (p.type) {
            case 'circle':
                ctx.beginPath();
                ctx.arc(p.x, p.y, p.size, 0, Math.PI * 2);
                ctx.fillStyle = p.color;
                ctx.fill();
                break;

            case 'rect':
                ctx.save();
                if (p.rotation) {
                    ctx.translate(p.x, p.y);
                    ctx.rotate(p.rotation * Math.PI / 180);
                    ctx.fillStyle = p.color;
                    ctx.fillRect(-p.size / 2, -p.size / 2, p.size, p.size * 0.6);
                    ctx.restore();
                } else {
                    ctx.fillStyle = p.color;
                    ctx.fillRect(p.x, p.y, p.size * 3, p.size);
                }
                break;

            case 'ring':
                ctx.beginPath();
                ctx.arc(p.x, p.y, p.size, 0, Math.PI * 2);
                ctx.strokeStyle = p.color;
                ctx.lineWidth = 1;
                ctx.stroke();
                break;

            case 'orbit':
                const ox = p.x + Math.cos(p.angle) * p.radius;
                const oy = p.y + Math.sin(p.angle) * p.radius;
                ctx.beginPath();
                ctx.arc(ox, oy, p.size, 0, Math.PI * 2);
                ctx.fillStyle = p.color;
                ctx.fill();
                break;

            case 'diamond':
                ctx.save();
                ctx.translate(p.x, p.y);
                ctx.rotate(Math.PI / 4);
                ctx.fillStyle = p.color;
                ctx.fillRect(-p.size / 2, -p.size / 2, p.size, p.size);
                ctx.restore();
                break;

            case 'star':
                ctx.beginPath();
                ctx.arc(p.x, p.y, p.size, 0, Math.PI * 2);
                ctx.fillStyle = p.color;
                ctx.fill();
                break;

            case 'text':
                ctx.font = `${p.size}px JetBrains Mono`;
                ctx.fillStyle = p.color;
                ctx.fillText(p.char, p.x, p.y);
                break;

            case 'neural':
                ctx.beginPath();
                ctx.arc(p.x, p.y, p.size, 0, Math.PI * 2);
                ctx.fillStyle = p.color;
                ctx.fill();
                // Draw line to target
                ctx.beginPath();
                ctx.moveTo(p.x, p.y);
                ctx.lineTo(p.targetX, p.targetY);
                ctx.strokeStyle = p.color;
                ctx.lineWidth = 0.5;
                ctx.stroke();
                break;
        }

        ctx.globalAlpha = 1;
    }

    stop() {
        if (this.animationId) {
            cancelAnimationFrame(this.animationId);
        }
    }
}
