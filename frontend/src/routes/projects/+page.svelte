<script lang="ts">
	// Browses the macros the user has actually saved on disk.
	//
	// A searchable grid, and one gesture: clicking a card loads that macro into
	// the workspace stores and takes the user there. There is no per-macro route
	// and no preview - the workspace itself is where a macro gets looked at.
	//
	// The listing comes from the Go runtime, which only exists once the app is
	// running, so it is fetched in the browser rather than at build time.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { FileWarning, Inbox, Loader, Search, SquareDashed, Unplug, X } from 'lucide-svelte';

	import { ListProjects, LoadProject } from '$lib/wailsjs/go/main/App';
	import type { main } from '$lib/wailsjs/go/models';
	import { openMacroInWorkspace } from '$lib/stores/flow';
	import { describeError } from '$lib/utils/helpers';
	import { isExpanded } from '$lib/stores/navbar';

	import TabTitle from '$lib/components/Projects/TabTitle.svelte';
	import MacroCard from '$lib/components/Projects/MacroCard.svelte';
	import '$lib/index.scss';

	// The list
	// --------

	type ListState =
		| { status: 'loading' }
		| { status: 'loaded'; macros: main.ProjectSummary[] }
		| { status: 'no-runtime' }
		| { status: 'error'; message: string };

	let listState: ListState = { status: 'loading' };

	/**
	 * Whether the Go side of the app is actually there.
	 *
	 * Used to explain a failure, never to pre-empt one: the listing is always
	 * attempted, so a runtime that turns up in some way this check does not
	 * recognise still gets to work. See `onMount`.
	 */
	const hasGoRuntime = (): boolean =>
		typeof window !== 'undefined' &&
		Boolean((window as { go?: { main?: { App?: unknown } } }).go?.main?.App);

	let query = '';

	$: macros = listState.status === 'loaded' ? listState.macros : [];

	// Search covers the id as well as the display name because the id is the
	// filename - a user who knows a macro as "mouse-jiggler.json" should find it
	// by typing that, even if they later renamed it to something prettier.
	$: needle = query.trim().toLowerCase();
	$: matches =
		needle === ''
			? macros
			: macros.filter(
					(macro) =>
						macro.name.toLowerCase().includes(needle) || macro.id.toLowerCase().includes(needle)
				);

	// Opening
	// -------

	type OpenState =
		| { status: 'idle' }
		| { status: 'opening'; id: string }
		| { status: 'error'; message: string };

	let openState: OpenState = { status: 'idle' };

	/**
	 * Loads a macro into the workspace and takes the user to it.
	 *
	 * `LoadProject` is a fresh parse that nothing else holds a reference to,
	 * which is what `openMacroInWorkspace` requires: node components mutate their
	 * `data` payload in place, so the workspace must never be handed objects
	 * another part of the app is also rendering.
	 */
	async function openInWorkspace(id: string): Promise<void> {
		if (openState.status === 'opening') return;
		openState = { status: 'opening', id };

		try {
			openMacroInWorkspace(await LoadProject(id));
			// The workspace is the root route; there is no URL that carries the
			// macro, which is why the graph is put in the stores first.
			await goto(`${base}/`);
		} catch (error) {
			openState = { status: 'error', message: describeError(error) };
		}
	}

	const isOpening = (state: OpenState, id: string): boolean =>
		state.status === 'opening' && state.id === id;

	const plural = (count: number, word: string): string =>
		`${count} ${word}${count === 1 ? '' : 's'}`;

	onMount(async () => {
		try {
			listState = { status: 'loaded', macros: await ListProjects() };
		} catch (error) {
			// The generated bindings reach straight into `window.go.main.App`, so
			// with the frontend served on its own - `npm run dev` in an ordinary
			// browser - this lands here with a bare "Cannot read properties of
			// undefined (reading 'main')". That is not a failure to read the
			// user's macros, and reporting it as one sends them looking for a
			// problem with their files; there is simply nothing here to read them
			// with. Anything else is a real error and keeps its message.
			listState = hasGoRuntime()
				? { status: 'error', message: describeError(error) }
				: { status: 'no-runtime' };
		}
	});
</script>

<TabTitle title="Macros" />

<!-- The navbar is fixed and can be collapsed from the workspace, and that
     collapse persists across routes. Reserving its height only while it is
     showing keeps the grid from starting under a bar that is not there. -->
<div class="macros-page" class:navbar-visible={$isExpanded}>
	<header class="macros-header">
		<h1 class="font-[var(--title-f)] text-2xl">Saved macros</h1>

		<div class="macros-search">
			<Search class="w-4 h-4 macros-search-icon" />
			<input
				class="macros-search-input"
				type="search"
				bind:value={query}
				placeholder="Search macros"
				aria-label="Search macros by name"
				autocomplete="off"
			/>
			{#if query}
				<button
					type="button"
					class="macros-search-clear"
					on:click={() => (query = '')}
					aria-label="Clear search"
				>
					<X class="w-4 h-4" />
				</button>
			{/if}
		</div>
	</header>

	{#if openState.status === 'error'}
		<p class="macros-open-error" role="alert">
			Could not open that macro: {openState.message}
		</p>
	{/if}

	{#if listState.status === 'loading'}
		<div class="macros-placeholder">
			<Loader class="w-8 h-8 macros-spinner" />
			<p class="font-300">Loading macros...</p>
		</div>
	{:else if listState.status === 'no-runtime'}
		<div class="macros-placeholder text-[var(--tertiary-text)]">
			<Unplug class="w-8 h-8" />
			<p class="font-300">Not connected to KeyPress.</p>
			<p class="font-300 text-sm">
				Macros are read off disk by the app itself, so this page has nothing to list when the
				frontend is served on its own. Run the desktop app to browse them.
			</p>
		</div>
	{:else if listState.status === 'error'}
		<div class="macros-placeholder text-[var(--accent-text)]">
			<FileWarning class="w-8 h-8" />
			<p class="font-300">Could not read your saved macros.</p>
			<p class="font-300 text-sm">{listState.message}</p>
		</div>
	{:else if macros.length === 0}
		<div class="macros-placeholder text-[var(--tertiary-text)]">
			<Inbox class="w-8 h-8" />
			<p class="font-300">No saved macros yet.</p>
			<p class="font-300 text-sm">Build a flow in the workspace, give it a name and hit save.</p>
		</div>
	{:else}
		<p class="macros-count font-300 text-sm text-[var(--tertiary-text)]">
			{#if needle}
				<!-- Spelt out rather than run through `plural`, which only knows how to
				     bolt an "s" on and would say "matchs". -->
				{matches.length === 1 ? '1 match' : `${matches.length} matches`} for
				&ldquo;{query.trim()}&rdquo;
			{:else}
				{plural(macros.length, 'macro')}
			{/if}
		</p>

		{#if matches.length === 0}
			<div class="macros-placeholder text-[var(--tertiary-text)]">
				<SquareDashed class="w-8 h-8" />
				<p class="font-300">Nothing matches &ldquo;{query.trim()}&rdquo;.</p>
			</div>
		{:else}
			<div class="macros-grid">
				{#each matches as macro (macro.id)}
					<MacroCard
						name={macro.name}
						nodeCount={macro.nodeCount}
						edgeCount={macro.edgeCount}
						modifiedAt={macro.modifiedAt}
						opening={isOpening(openState, macro.id)}
						on:click={() => openInWorkspace(macro.id)}
					/>
				{/each}
			</div>
		{/if}
	{/if}
</div>

<style lang="scss">
	.macros-page {
		display: flex;
		flex-direction: column;
		gap: 1rem;
		box-sizing: border-box;
		min-height: 100vh;
		padding: 1.5rem 1.5rem 2.5rem;
		color: var(--main-text);
		// index.scss transitions every element but does not list padding, and this
		// one moves whenever the navbar is toggled.
		transition: padding-top 300ms ease;
	}

	.macros-page.navbar-visible {
		padding-top: 5rem; /* 4rem navbar + the page's own 1rem of breathing room */
	}

	.macros-header {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem 1.5rem;
	}

	.macros-search {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex: 1 1 220px;
		max-width: 22rem;
		padding: 0.5rem 0.75rem;
		background: var(--main);
		border: 1px solid var(--border);
		border-radius: 10px;

		&:focus-within {
			border-color: var(--link);
			box-shadow: 0 0 0 1px var(--link);
		}
	}

	.macros-search :global(.macros-search-icon) {
		flex-shrink: 0;
		color: var(--tertiary-text);
	}

	.macros-search-input {
		flex: 1;
		min-width: 0;
		background: transparent;
		border: none;
		outline: none;
		color: var(--main-text);
		font-weight: 300;

		// Chromium draws its own clear affordance on type="search"; this page has
		// its own button, and two of them side by side is a mess.
		&::-webkit-search-cancel-button {
			display: none;
		}
	}

	.macros-search-clear {
		display: flex;
		flex-shrink: 0;
		cursor: pointer;
		color: var(--tertiary-text);

		&:hover {
			color: var(--main-text);
		}
	}

	.macros-open-error {
		color: var(--error);
		font-weight: 300;
		font-size: 0.875rem;
	}

	.macros-placeholder {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.75rem;
		padding: 3rem 1rem;
		text-align: center;
	}

	// The keyframes are global (index.scss); this only names them, so that the
	// markup does not have to carry an inline `style` to spin an icon.
	.macros-placeholder :global(.macros-spinner) {
		animation: spin 1s linear infinite;
	}

	.macros-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
		gap: var(--grid-gap, 0.75rem);
		align-content: start;
		// Room for a focused card's outline, which sits outside its border box.
		padding: 2px;
	}
</style>
