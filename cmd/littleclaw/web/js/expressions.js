/**
 * Littleclaw Face - Expression Definitions
 * Maps expression names to their configuration.
 */
const EXPRESSIONS = {
    idle: {
        className: 'face-idle',
        icon: '',
        label: 'Idle',
        glowColor: '#6366f1',
        particleType: 'dust'
    },
    thinking: {
        className: 'face-thinking',
        icon: '...',
        label: 'Thinking',
        glowColor: '#6366f1',
        particleType: 'dots'
    },
    speaking: {
        className: 'face-speaking',
        icon: '>',
        label: 'Speaking',
        glowColor: '#10b981',
        particleType: 'text'
    },
    listening: {
        className: 'face-listening',
        icon: '~',
        label: 'Listening',
        glowColor: '#3b82f6',
        particleType: 'waves'
    },
    working: {
        className: 'face-working',
        icon: '#',
        label: 'Working',
        glowColor: '#f59e0b',
        particleType: 'sparks'
    },
    searching: {
        className: 'face-searching',
        icon: '?',
        label: 'Searching',
        glowColor: '#3b82f6',
        particleType: 'orbit'
    },
    remembering: {
        className: 'face-remembering',
        icon: '*',
        label: 'Remembering',
        glowColor: '#a855f7',
        particleType: 'float'
    },
    sleeping: {
        className: 'face-sleeping',
        icon: 'z',
        label: 'Sleeping',
        glowColor: '#55556a',
        particleType: 'stars'
    },
    excited: {
        className: 'face-excited',
        icon: '!',
        label: 'Excited',
        glowColor: '#10b981',
        particleType: 'confetti'
    },
    confused: {
        className: 'face-confused',
        icon: '?!',
        label: 'Confused',
        glowColor: '#f43f5e',
        particleType: 'question'
    },
    consolidating: {
        className: 'face-consolidating',
        icon: '~',
        label: 'Consolidating',
        glowColor: '#a855f7',
        particleType: 'neural'
    }
};

/**
 * Tool name to icon mapping for the thought bubble
 */
const TOOL_ICONS = {
    web_search: '[search]',
    web_fetch: '[fetch]',
    exec: '[shell]',
    read_file: '[read]',
    write_file: '[write]',
    append_file: '[append]',
    update_core_memory: '[memory]',
    write_entity: '[entity]',
    read_entity: '[entity]',
    list_entities: '[list]',
    add_cron: '[cron+]',
    remove_cron: '[cron-]',
    list_cron: '[cron]',
    send_telegram_file: '[send]',
    reload_skills: '[skills]'
};

/**
 * Activity kind to display configuration
 */
const ACTIVITY_KINDS = {
    tool_call: { color: '#f59e0b', label: 'Tool' },
    message_in: { color: '#3b82f6', label: 'User' },
    message_out: { color: '#10b981', label: 'Bot' },
    cron_fired: { color: '#a855f7', label: 'Cron' },
    cron_completed: { color: '#a855f7', label: 'Cron' },
    heartbeat: { color: '#f43f5e', label: 'HB' },
    consolidation: { color: '#a855f7', label: 'Mem' },
    thinking_start: { color: '#6366f1', label: 'Think' },
    thinking_end: { color: '#6366f1', label: 'Done' },
    tool_result: { color: '#f59e0b', label: 'Result' },
    response_ready: { color: '#10b981', label: 'Reply' },
    memory_write: { color: '#f59e0b', label: 'Mem' },
    entity_update: { color: '#a855f7', label: 'Entity' }
};
