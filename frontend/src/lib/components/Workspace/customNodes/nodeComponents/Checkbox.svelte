<script lang="ts">
    export let label: string;
    export let checked: boolean = false;
    export let disabled: boolean = false;
    export let highlightColor: string = 'bg-gray-500';
    export let id = crypto.randomUUID();
    import '$lib/index.scss'
</script>

<div class="checkbox-container select-none">
    <label 
        for={id} 
        class="flex items-center {disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}"
    >
        <span class="mr-2 text-sm --main-text">{label}</span>
        <input
            {id}
            type="checkbox"
            bind:checked
            {disabled}
            class="sr-only absolute"
            aria-label={label}
        />
        <div class="checkbox-custom relative {checked ? highlightColor : 'bg-white'}">
            {#if checked}
                <svg 
                    class="check-mark" 
                    viewBox="0 0 12 12" 
                    fill="none" 
                    xmlns="http://www.w3.org/2000/svg"
                >
                    <path 
                        d="M10 3L4.5 8.5L2 6" 
                        stroke="currentColor" 
                        stroke-width="2" 
                        stroke-linecap="round" 
                        stroke-linejoin="round"
                    />
                </svg>
            {/if}
        </div>
    </label>
</div>

<style>
    .checkbox-container {
        display: inline-block;
        margin: 0;
    }

    .checkbox-custom {
        width: 18px;
        height: 18px;
        border: 2px solid var(--accent);
        border-radius: 4px;
        display: flex;
        align-items: center;
        justify-content: center;
        transition: all 0.2s ease;
    }

    .check-mark {
        position: absolute;
        width: 12px;
        height: 12px;
        color: white;
        left: 50%;
        top: 50%;
        transform: translate(-50%, -50%);
    }
</style>