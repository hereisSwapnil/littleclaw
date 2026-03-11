/**
 * Littleclaw Face - SVG Face Controller
 * Manages expression morphing and face animations.
 */
class FaceController {
    constructor() {
        this.faceContainer = document.getElementById('face-container');
        this.faceSvg = document.getElementById('face-svg');
        this.faceGlow = document.getElementById('face-glow');
        this.thoughtBubble = document.getElementById('thought-bubble');
        this.thoughtText = document.getElementById('thought-text');
        this.thoughtIcon = document.getElementById('thought-icon');
        this.expressionLabel = document.getElementById('expression-label');

        // this.currentExpression = 'idle';
        this.currentExpression = 'idle';
        this.currentClassName = 'face-idle';

        // Apply initial expression
        this.faceContainer.classList.add('face-idle');
    }

    /**
     * Set the face expression with smooth transition
     */
    setExpression(expression) {
        if (expression === this.currentExpression) return;

        const config = EXPRESSIONS[expression] || EXPRESSIONS.idle;

        // Remove old expression class
        this.faceContainer.classList.remove(this.currentClassName);

        // Add new expression class
        this.faceContainer.classList.add(config.className);
        this.currentClassName = config.className;
        this.currentExpression = expression;

        // Update expression label
        this.expressionLabel.textContent = config.label;
        this.expressionLabel.style.color = config.glowColor;

        return config;
    }

    /**
     * Show the thought bubble with text
     */
    showThought(text, icon) {
        if (!text) {
            this.hideThought();
            return;
        }

        this.thoughtText.textContent = text;
        this.thoughtIcon.textContent = icon || '';
        this.thoughtBubble.classList.add('visible');
    }

    /**
     * Hide the thought bubble
     */
    hideThought() {
        this.thoughtBubble.classList.remove('visible');
    }

    /**
     * Update the face based on a state snapshot from the server
     */
    updateFromState(state) {
        const config = this.setExpression(state.expression);

        if (state.thought) {
            const icon = state.action ? (TOOL_ICONS[state.action] || '[*]') : (config ? config.icon : '');
            this.showThought(state.thought, icon);
        } else {
            this.hideThought();
        }
    }
}
