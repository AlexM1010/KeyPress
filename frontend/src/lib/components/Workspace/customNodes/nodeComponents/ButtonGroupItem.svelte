<!-- ButtonGroupItem.svelte -->
<script lang="ts">
    import { getContext, onMount, onDestroy } from 'svelte';
    import { cn } from '$lib/utils/buttonUtils';
    import { derived } from 'svelte/store';
    import "$lib/index.scss";

    interface ButtonGroupContext {
        register: (id: string) => void;
        unregister: (id: string) => void;
        items: import('svelte/store').Writable<string[]>;
    }

    export let value: any = undefined;
    export let className: string | undefined = undefined;
    export { className as class };

    export let active: boolean = false;
    export let disabled: boolean = false;
    export let itemHighlightColor: string = '';

    export let variant: 'default' | 'danger' = 'default';

    // Get context from ButtonGroup
    const context: ButtonGroupContext = getContext('buttonGroupCtx');

    // Unique ID for the button, can be the `value` or any unique identifier
    const id = value;

    // Register and unregister the button on mount and destroy
    onMount(() => {
        context.register(id);
    });

    onDestroy(() => {
        context.unregister(id);
    });

    // Determine position based on the items store
    let position: 'first' | 'middle' | 'last' | 'only' = 'middle';

    // Create a derived store to get the list of items
    const derivedPosition = derived(context.items, ($items) => {
        if ($items.length === 1) return 'only';
        if ($items[0] === id) return 'first';
        if ($items[$items.length - 1] === id) return 'last';
        return 'middle';
    });

    // Subscribe to the derived store to get the position
    derivedPosition.subscribe(pos => {
        position = pos;
    });

    // Determine effective highlight color
    $: effectiveHighlightColor = itemHighlightColor || 'bg-gray-500';

    // Compute the button classes based on props and position
    $: buttonClass = [
        'py-2 px-4 transition-all duration-200 text-sm',
        'flex-1',
        active ? getActiveClass() : getInactiveClass(),
        disabled ? 'opacity-50 cursor-not-allowed' : '',
        className,
        position === 'first' ? 'rounded-l-lg' : '',
        position === 'last' ? 'rounded-r-lg' : '',
        position === 'only' ? 'rounded-lg' : '',
    ].filter(Boolean).join(' ');

    function getActiveClass() {
        if (variant === 'danger') {
            return 'bg-red-500 text-white hover:bg-red-600';
        }
        return `${effectiveHighlightColor} text-white hover:bg-opacity-90`;
    }

    function getInactiveClass() {
        return `bg-[var(--main)] hover:bg-[var(--secondary-hover)] --main-text`;
    }
</script>
  
<button
    class={buttonClass}
    disabled={disabled}
    on:click
>
    <slot />
</button>
  