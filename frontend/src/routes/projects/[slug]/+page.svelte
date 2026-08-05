<script lang="ts">
  // Previews one saved macro: its own node graph, read-only.
  import { page } from '$app/stores';
  import { base } from '$app/paths';
  import { writable } from 'svelte/store';
  import { SvelteFlowProvider, type Edge } from '@xyflow/svelte';
  import { ArrowLeft, FileWarning, Inbox, Loader } from 'lucide-svelte';

  import { LoadProject } from '$lib/wailsjs/go/main/App';
  import type { FlowNode } from '$lib/stores/flow';

  import TabTitle from '$lib/components/Projects/TabTitle.svelte';
  import CardDivider from '$lib/components/Projects/Card/CardDivider.svelte';
  import Flow from '../../workspace/Flow.svelte';
  import '$lib/index.scss';

  /**
   * Stores that back the preview graph.
   *
   * These are this page's own instances, never the `nodesData` / `edgesData`
   * the workspace uses. Custom node components mutate their `data` payload in
   * place, and that write goes through to whichever store holds the node - so
   * sharing the workspace stores here would mean previewing a macro could edit
   * the flow the user has open. They are seeded purely from the JSON that
   * `LoadProject` returns, which is a fresh parse on every call.
   */
  const previewNodes = writable<FlowNode[]>([]);
  const previewEdges = writable<Edge[]>([]);

  type LoadState =
    | { status: 'loading' }
    | { status: 'loaded'; name: string; nodeCount: number }
    | { status: 'error'; message: string };

  let state: LoadState = { status: 'loading' };

  async function loadProject(id: string): Promise<void> {
    state = { status: 'loading' };
    try {
      const data = await LoadProject(id);

      // The Go model types position as a loose number map, so narrow it back
      // to the { x, y } shape Svelte Flow expects - same as the workspace does
      // for LoadLastFile.
      const nodes: FlowNode[] = (data.nodes ?? []).map((node) => ({
        id: node.id,
        type: node.type,
        data: node.data ?? {},
        position: { x: node.position?.x ?? 0, y: node.position?.y ?? 0 }
      }));

      const edges: Edge[] = data.edges ?? [];

      previewNodes.set(nodes);
      previewEdges.set(edges);

      state = { status: 'loaded', name: data.name ?? id, nodeCount: nodes.length };
    } catch (error) {
      previewNodes.set([]);
      previewEdges.set([]);
      state = {
        status: 'error',
        message: error instanceof Error ? error.message : String(error)
      };
    }
  }

  // SvelteKit reuses this component across /projects/<a> -> /projects/<b>, so
  // the load is keyed off the param rather than done once on mount.
  $: slug = $page.params.slug;
  $: if (slug) void loadProject(slug);

  $: pageTitle = state.status === 'loaded' ? state.name : 'Macro';
</script>

<TabTitle title={pageTitle} />

<div class="macro-page">
  <a class="macro-back" href={`${base}/projects`}>
    <ArrowLeft class="w-4 h-4" />
    <span>All macros</span>
  </a>

  {#if state.status === 'loading'}
    <div class="macro-placeholder">
      <Loader class="w-8 h-8" style="animation: spin 1s linear infinite" />
      <p class="font-300">Loading macro...</p>
    </div>
  {:else if state.status === 'error'}
    <div class="macro-placeholder text-[var(--accent-text)]">
      <FileWarning class="w-8 h-8" />
      <p class="font-300">Could not load this macro.</p>
      <p class="font-300 text-sm">{state.message}</p>
    </div>
  {:else}
    <h1 class="font-[var(--title-f)] text-2xl">{state.name}</h1>
    <CardDivider />
    {#if state.nodeCount === 0}
      <div class="macro-placeholder text-[var(--tertiary-text)]">
        <Inbox class="w-8 h-8" />
        <p class="font-300">This macro has no nodes.</p>
      </div>
    {:else}
      <!-- Keyed so each macro gets a fresh flow instance, and with it a fresh
           fitView onto its own graph. -->
      {#key slug}
        <div class="macro-preview">
          <SvelteFlowProvider>
            <Flow previewMode={true} nodes={previewNodes} edges={previewEdges} />
          </SvelteFlowProvider>
        </div>
      {/key}
    {/if}
  {/if}
</div>

<style lang="scss">
  .macro-page {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    padding: 5rem 1.5rem 2.5rem;
    color: var(--main-text);
  }

  .macro-back {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    align-self: flex-start;
    color: var(--tertiary-text);
    text-decoration: none;
    font-weight: 300;

    &:hover {
      color: var(--main-text);
    }
  }

  .macro-placeholder {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.75rem;
    padding: 3rem 1rem;
    text-align: center;
  }

  .macro-preview {
    height: 60vh;
    min-height: 320px;
    width: 100%;
    overflow: hidden;
    border: 1px solid var(--border);
    border-radius: 15px;
  }

  .macro-preview :global(.flow-container) {
    height: 100%;
  }
</style>
