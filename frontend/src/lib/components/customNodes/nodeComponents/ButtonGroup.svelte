<!-- ButtonGroup.svelte -->
<script lang="ts">
    import { setContext } from 'svelte';
    import { writable } from 'svelte/store';
    import { cn } from '$lib/utils/buttonUtils';
  
    export const variant: 'default' | 'danger' = 'default';
    export const size: 'sm' | 'md' | 'lg' = 'md';
    export let className: string = '';
    export { className as class };
  
    // Create a store to track the order of ButtonGroupItems
    const items = writable<string[]>([]);

    // Silence warnings for unknown props
    $$restProps;
  
    // Provide context to child ButtonGroupItems
    setContext('buttonGroupCtx', {
        register: (id: string) => {
            items.update(current => [...current, id]);
        },
        unregister: (id: string) => {
            items.update(current => current.filter(item => item !== id));
        },
        items
    });
</script>
  
<div class={cn("flex items-center justify-center button-group", className)}>
    <slot />
</div>