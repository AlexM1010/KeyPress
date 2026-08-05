<!-- src/routes/ThemeToggle.svelte -->
<script lang="ts">
    import Sun from 'lucide-svelte/icons/sun';
    import Moon from 'lucide-svelte/icons/moon';
    import { theme, toggleTheme } from '$lib/stores/theme';

    let spin = false;

    // Reading `$theme` also keeps the store subscribed for this component's
    // lifetime, which is what keeps <html data-theme> in sync (see theme.ts).
    function toggleColorScheme() {
        toggleTheme(!$theme);
        triggerSpin(300);
    }

    function triggerSpin(duration: number) {
        spin = true;
        setTimeout(() => (spin = false), duration);
    }
</script>

<style>
    .theme-toggle-btn {
        background: transparent;
        border: none;
        padding: 0.5rem;
        cursor: pointer;
        border-radius: 0.5rem;
    }

    .spin {
        animation: spin 0.3s linear infinite;
    }

    @keyframes spin {
        0% { transform: rotate(0deg); }
        100% { transform: rotate(360deg); }
    }
</style>

<button
    class="theme-toggle-btn"
    on:click={toggleColorScheme}
    type="button"
    aria-label="Toggle theme"
>
    <div class:spin={spin} class="relative flex items-center justify-center">
        <Sun class="h-[1.2rem] w-[1.2rem] rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0" />
        <Moon class="color: stroke-white absolute h-[1.2rem] w-[1.2rem] rotate-90 scale-0 transition-all dark:rotate-0 dark:scale-100" />
        <span class="sr-only">Toggle theme</span>
    </div>
</button>