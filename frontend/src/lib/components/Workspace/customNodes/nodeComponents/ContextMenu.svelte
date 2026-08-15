<script lang="ts">
    import { Trash2, Copy } from "lucide-svelte";
    import { theme } from "$lib/stores/theme.svelte";

    // Callback props rather than createEventDispatcher.
    //
    // Component events are removed in Svelte 5, and callback props are their
    // replacement - but they already work in Svelte 4, so this crosses that
    // break now rather than during the migration. Nothing else about the
    // component changes: NodeWrapper passes the same two functions it used to
    // hand to `on:duplicate` / `on:delete`.
    export let onDuplicate: () => void;
    export let onDelete: () => void;
</script>

<div class={`context-menu ${$theme ? "dark" : ""}`}>
    <div class="button-container">
        <button
            on:click={onDuplicate}
            aria-label="Duplicate"
            class="icon-button"
        >
            <Copy class="icon" />
        </button>
        <button
            on:click={onDelete}
            aria-label="Delete"
            class="icon-button"
        >
            <Trash2 class="icon" />
        </button>
    </div>
</div>

<style>
    .context-menu {
        --icon-color: #4a5568;
        --icon-hover-color: #2d3748;

        padding: 0.5rem;
        animation: slideIn 200ms cubic-bezier(0.16, 1, 0.3, 1) forwards;
        min-width: fit-content;
    }

    .button-container {
        display: flex;
        flex-direction: row;
        min-width: fit-content;
        gap: 0.5rem;
    }

    .icon-button {
        background: none;
        border: none;
        cursor: pointer;
        color: var(--icon-color, #4a5568);

        padding: 0.5rem;
        border-radius: 0.5rem;
        transition: all 0.2s ease;
        width: 36px;
        height: 36px;
    }

    .icon {
        width: 24px;
        height: 24px;
    }

    .context-menu.dark {
        --icon-color: #9ca3af;
        --icon-hover-color: #d1d5db;
    }

    .icon-button:hover {
        color: var(--icon-hover-color);
    }

    @keyframes slideIn {
        from {
            transform: translateY(12px);
            opacity: 0;
        }
        to {
            transform: translateY(0);
            opacity: 1;
        }
    }
</style>
