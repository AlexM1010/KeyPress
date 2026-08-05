<script lang="ts">
    import { Trash2, Copy } from "lucide-svelte";
    import { createEventDispatcher } from "svelte";
    import { theme } from "$lib/stores/theme";

    const dispatch = createEventDispatcher();

    function handleDuplicate() {
        dispatch("duplicate");
    }

    function handleDelete() {
        dispatch("delete");
    }
</script>

<div class="context-menu-wrapper">
    <div class={`context-menu ${$theme ? "dark" : ""}`}>
        <div class="button-container">
            <button
                on:click={handleDuplicate}
                aria-label="Duplicate"
                class="icon-button"
            >
                <Copy class="icon" />
            </button>
            <button
                on:click={handleDelete}
                aria-label="Delete"
                class="icon-button"
            >
                <Trash2 class="icon" />
            </button>
        </div>
    </div>
</div>

<style>
    :global().context-menu-wrapper {
        position: absolute;
        top: -50px;
        right: 100px;
        min-width: fit-content;
    }

    .context-menu {
        --icon-color: #4a5568;
        --icon-hover-color: #2d3748;

        padding: 0.5rem;
        animation: slideIn 200ms cubic-bezier(0.16, 1, 0.3, 1) forwards;
        min-width: fit-content;
        position: absolute;
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
            transform: translateY(50px);
            z-index: -10;
        }
        50% {
            z-index: 0;
        }
        to {
            transform: translateY(0);
            z-index: 100;
        }
    }
</style>
