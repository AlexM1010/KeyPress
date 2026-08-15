<!-- frontend/src/lib/test/NodeHarness.svelte -->
<script lang="ts">
    import type { ComponentType, SvelteComponent } from 'svelte';
    import { setContext } from 'svelte';
    import { writable } from 'svelte/store';
    import { SvelteFlowProvider } from '@xyflow/svelte';

    /**
     * Enough of a Svelte Flow canvas for one custom node to mount outside one.
     *
     * `NodeWrapper` calls `useSvelteFlow()` and renders `<Handle>`, and both read
     * the flow store out of context - so a node rendered bare throws before it
     * paints anything. `<SvelteFlowProvider>` is the library's own answer to
     * that: it builds the same store `<SvelteFlow>` would and puts it in context,
     * without the renderer, the viewport or the pane. That is the whole reason
     * these tests can drive a node's controls without standing up a canvas, a
     * zoom transform and a `ResizeObserver` around it.
     *
     * The two node-level contexts are set here rather than by the provider
     * because `<SvelteFlow>` normally sets them per node as it renders it, and
     * nothing is rendering nodes here. `svelteflow__node_connectable` has to be a
     * *store*: `Handle` reads it as `$connectable` whenever the node does not
     * pass `isConnectable` itself.
     */
    // `any` props for the same reason `nodeHarness.ts` explains at length: under
    // Svelte 5 a component's constructor options are invariant in its props, so
    // nothing with a required prop is assignable to a parameter typed for a
    // component with arbitrary ones - and taking *any* node is this component's
    // entire purpose.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    export let component: ComponentType<SvelteComponent<any, any, any>>;
    export let props: Record<string, unknown> = {};

    setContext('svelteflow__node_id', props.id);
    setContext('svelteflow__node_connectable', writable(true));
</script>

<SvelteFlowProvider>
    <svelte:component this={component} {...props} />
</SvelteFlowProvider>
