<!-- Select.svelte -->
<script lang="ts">
    import type { SvelteComponent } from 'svelte';

    export let label: string;
    export let options: string[];
    export let icon: typeof SvelteComponent<any> | null = null;
    export let value: string = '';

    // The DOM id of the <select>, tying it to its <label for=...>. It used to be
    // the hard-coded literal "select-input", so every instance on the page
    // rendered the same id: with two Selects in one node the document had
    // duplicate ids and *both* labels resolved to the first control, so clicking
    // the second label focused the wrong one. Defaulting to a fresh unique value
    // per instance (the same approach Slider.svelte already takes) keeps the
    // label/control pairing correct however many are on screen, while callers
    // that need a stable, predictable id can still pass one in.
    export let id: string = `select-${crypto.randomUUID()}`;
</script>

<div class="space-y-1.5">
    <label for={id} class="block text-xs font-medium --main-text">{label}</label>
    <div class="relative">
        {#if icon}
            <div class="absolute left-3 top-1/2 transform -translate-y-1/2 --main-text">
                <svelte:component this={icon} class="w-4 h-4" />
            </div>
        {/if}
        <select
            {id}
            bind:value
            class="w-full pr-3 py-2 pl-10 text-sm bg-gray-50 border border-gray-200 rounded-lg focus:ring-2 focus:ring-blue-400 focus:border-transparent"
        >
            {#each options as opt}
                <option value={opt}>{opt}</option>
            {/each}
        </select>
    </div>
</div>
