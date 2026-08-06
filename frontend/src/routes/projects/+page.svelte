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
	import {
		FileWarning,
		Inbox,
		Loader,
		Search,
		SquareDashed,
		TriangleAlert,
		Unplug,
		X
	} from 'lucide-svelte';

	import { ListProjects, LoadProject } from '$lib/wailsjs/go/backend/App';
	import type { backend } from '$lib/wailsjs/go/models';
	import { openMacroInWorkspace, isMacroDirty, macroName } from '$lib/stores/flow';
	import { describeError, hasGoRuntime } from '$lib/utils/helpers';

	import TabTitle from '$lib/components/Projects/TabTitle.svelte';
	import MacroCard from '$lib/components/Projects/MacroCard.svelte';
	import '$lib/index.scss';

	// The list
	// --------

	type ListState =
		| { status: 'loading' }
		| { status: 'loaded'; macros: backend.ProjectSummary[] }
		| { status: 'no-runtime' }
		| { status: 'error'; message: string };

	let listState: ListState = { status: 'loading' };

	// `hasGoRuntime` is shared with the workspace (`$lib/utils/helpers`), which
	// needs the same distinction when a save fails. It explains a failure and
	// never pre-empts one: the listing is always attempted, so a runtime that
	// turns up in some way the check does not recognise still gets to work. See
	// `onMount`.

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
	 * The macro a click asked for while the workspace still held unsaved edits,
	 * held back until the user says what to do about them. `null` when there is
	 * nothing waiting.
	 *
	 * Opening replaces the workspace graph outright, so this is the one gesture
	 * on this page that can destroy work the user has not saved - and until now
	 * it did so on a single click with nothing said.
	 */
	let pendingOpen: { id: string; name: string } | null = null;

	/**
	 * Opens a macro, or asks first if that would throw away unsaved edits.
	 *
	 * The dirty flag is maintained by the workspace, which is the only place a
	 * graph can be edited; it keeps its last value while the user is over here,
	 * so it still describes the canvas they left.
	 */
	function requestOpen(macro: { id: string; name: string }): void {
		if (openState.status === 'opening') return;

		if ($isMacroDirty) {
			pendingOpen = macro;
			return;
		}
		openInWorkspace(macro.id);
	}

	function confirmPendingOpen(): void {
		if (!pendingOpen) return;
		const { id } = pendingOpen;
		pendingOpen = null;
		openInWorkspace(id);
	}

	/**
	 * Loads a macro into the workspace and takes the user to it.
	 *
	 * `LoadProject` is a fresh parse that nothing else holds a reference to,
	 * which is what `openMacroInWorkspace` requires: node components mutate their
	 * `data` payload in place, so the workspace must never be handed objects
	 * another part of the app is also rendering.
	 *
	 * Whatever was on the canvas is gone once this succeeds, which is why every
	 * caller comes through `requestOpen`.
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

	// What the workspace currently holds, for the warning to name. A macro that
	// has never been saved has no name to give, so it is described instead - the
	// user still needs to know which graph is about to go.
	$: openMacroLabel = $macroName.trim() === '' ? 'your unsaved macro' : `"${$macroName.trim()}"`;

	onMount(async () => {
		try {
			listState = { status: 'loaded', macros: await ListProjects() };
		} catch (error) {
			// The generated bindings reach straight into `window.go.backend.App`,
			// so with the frontend served on its own - `npm run dev` in an ordinary
			// browser - this lands here with a bare "Cannot read properties of
			// undefined (reading 'backend')". That is not a failure to read the
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

<!-- The navbar is fixed, so the grid reserves its height to avoid starting
     underneath it. -->
<div class="macros-page">
	<!-- The page carries no visible heading, so the field's own label is what
	     names it - to a screen reader and to anything else reading the page. -->
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

	{#if openState.status === 'error'}
		<p class="macros-open-error" role="alert">
			Could not open that macro: {openState.message}
		</p>
	{/if}

	<!-- Unsaved edits in the workspace, and a click that would replace them.
	     In the page rather than a `confirm()` dialog: the webview's own dialog is
	     styled by the platform, cannot name the macro without quoting oddities,
	     and blocks the whole UI to ask a question the user may want to answer by
	     going back and saving. -->
	{#if pendingOpen}
		<div class="macros-confirm" role="alertdialog" aria-labelledby="macros-confirm-message">
			<p id="macros-confirm-message" class="macros-confirm-message">
				<TriangleAlert class="w-4 h-4 macros-confirm-icon" />
				<span>
					You have unsaved changes in {openMacroLabel}. Opening &ldquo;{pendingOpen.name}&rdquo;
					will discard them.
				</span>
			</p>
			<div class="macros-confirm-actions">
				<!-- Cancel first and focused: the safe answer should be the one a
				     stray Enter gives. -->
				<!-- svelte-ignore a11y-autofocus -->
				<button
					type="button"
					class="macros-confirm-button"
					autofocus
					on:click={() => (pendingOpen = null)}
				>
					Keep editing
				</button>
				<button
					type="button"
					class="macros-confirm-button macros-confirm-button-danger"
					on:click={confirmPendingOpen}
				>
					Discard and open
				</button>
			</div>
		</div>
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
	{:else if matches.length === 0}
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
					on:click={() => requestOpen({ id: macro.id, name: macro.name })}
				/>
			{/each}
		</div>
	{/if}
</div>

<style lang="scss">
	.macros-page {
		display: flex;
		flex-direction: column;
		gap: 1rem;
		box-sizing: border-box;
		min-height: 100vh;
		/* 5rem top: 4rem navbar + the page's own 1rem of breathing room */
		padding: 5rem 1.5rem 2.5rem;
		color: var(--main-text);
	}

	.macros-search {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		width: 66.6667%;
		// `auto` on both sides centres it in the page's content box; the page is
		// a flex column, so this is what stops it stretching the full width.
		margin-inline: auto;
		padding: 0.5rem 0.75rem;
		background: var(--main);
		border: 1px solid var(--border);
		border-radius: 10px;

		&:focus-within {
			border-color: var(--link);
			box-shadow: 0 0 0 1px var(--link);
		}

		// Two thirds of a narrow window leaves too little to type into, so below
		// this the field takes the width it has.
		@media (max-width: 640px) {
			width: 100%;
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

	// The unsaved-changes warning. Sits above the grid, in the flow of the page,
	// so the cards stay visible behind the question rather than being covered by
	// a modal the user has to dismiss to remember what they were choosing.
	.macros-confirm {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
		padding: 0.75rem 1rem;
		background: var(--main);
		border: 1px solid var(--link);
		border-radius: 10px;
	}

	.macros-confirm-message {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		margin: 0;
		font-weight: 300;
		font-size: 0.875rem;
		color: var(--main-text);
	}

	.macros-confirm :global(.macros-confirm-icon) {
		flex-shrink: 0;
		color: var(--link);
	}

	.macros-confirm-actions {
		display: flex;
		gap: 0.5rem;
		margin-left: auto;
	}

	.macros-confirm-button {
		padding: 0.4rem 0.8rem;
		border: 1px solid var(--border);
		border-radius: 0.25rem;
		background: var(--tertiary);
		color: var(--main-text);
		font-size: 0.875rem;
		font-weight: 300;
		cursor: pointer;

		&:hover {
			background: var(--tertiary-hover);
		}
	}

	// The destructive answer looks destructive. It is deliberately the plainer
	// of the two: the button that loses work should not be the one the eye goes
	// to first.
	.macros-confirm-button-danger {
		border-color: var(--error);
		color: var(--error);
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
