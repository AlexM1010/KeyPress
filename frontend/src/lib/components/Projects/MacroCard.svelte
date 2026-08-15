<script lang="ts">
	// One saved macro in the grid.
	//
	// A <button>, not a link: activating it loads the macro into the workspace
	// stores and then navigates, so there is no URL that describes the result and
	// an <a> would promise one. Being a button also puts the card in the tab
	// order and makes it work from the keyboard for free. The click is forwarded
	// to the parent unchanged.
	import { Clock, Keyboard, Loader, Waypoints, Workflow } from 'lucide-svelte';

	export let name: string;
	export let nodeCount: number;
	export let edgeCount: number;
	export let modifiedAt: string;

	/**
	 * The global hotkey currently registered for this macro, empty when it has
	 * none.
	 *
	 * Shown, never set. A macro's trigger is recorded on its Start node in the
	 * workspace - that is the only place it is configured, and the only place it
	 * is stored. Offering a second way to change it here is what previously let
	 * a hotkey recorded on the node sit there doing nothing while this card
	 * claimed a different one.
	 */
	export let hotkey = '';

	/** This macro is being loaded into the workspace right now. */
	export let opening = false;

	const plural = (count: number, word: string): string =>
		`${count} ${word}${count === 1 ? '' : 's'}`;

	/**
	 * A modification time is only as good as its parse: `ListProjects` leaves it
	 * empty when the file's timestamp could not be read, and a file written by
	 * something else could hold anything at all. Both cases say so rather than
	 * render "Invalid Date".
	 */
	function formatModified(value: string): string {
		if (!value) return 'Date unknown';
		const date = new Date(value);
		if (Number.isNaN(date.getTime())) return 'Date unknown';
		return date.toLocaleString(undefined, {
			day: 'numeric',
			month: 'short',
			year: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
	}
</script>

<!-- The card and its hotkey line are siblings inside one grid cell, not nested:
     the hotkey belongs to the macro but is not part of the button that opens
     it, and reads better under it than crammed into the stats row. -->
<div class="macro-card-slot">
	<button type="button" class="macro-card" title={`Open "${name}" in the workspace`} on:click>
		<span class="macro-card-head">
			<span class="macro-card-icon">
				{#if opening}
					<!-- The keyframes are global (index.scss); naming them from the style
				     block below keeps an inline `style` out of the markup. -->
					<Loader class="w-4 h-4 macro-card-spinner" />
				{:else}
					<Workflow class="w-4 h-4" />
				{/if}
			</span>
			<span class="macro-card-name">{name}</span>
		</span>

		<span class="macro-card-meta">
			<span class="macro-card-stat">
				<Waypoints class="w-3.5 h-3.5" />
				{plural(nodeCount, 'node')}
			</span>
			<span class="macro-card-stat">
				<Workflow class="w-3.5 h-3.5" />
				{plural(edgeCount, 'connection')}
			</span>
		</span>

		<span class="macro-card-stat macro-card-modified">
			<Clock class="w-3.5 h-3.5" />
			{formatModified(modifiedAt)}
		</span>
	</button>

	{#if hotkey}
		<p class="macro-hotkey">
			<Keyboard class="w-3.5 h-3.5 macro-hotkey-icon" />
			<kbd class="macro-hotkey-combo">{hotkey}</kbd>
		</p>
	{/if}
</div>

<style lang="scss">
	.macro-card-slot {
		display: flex;
		flex-direction: column;
		gap: 0.35rem;
		min-width: 0;
	}

	.macro-card {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		width: 100%;
		padding: 0.9rem 1rem;
		text-align: left;
		cursor: pointer;
		color: var(--main-text);
		background: var(--main);
		border: 1px solid var(--border);
		border-radius: 12px;
		// The panel's own transition list covers colours and shadow; transform is
		// this component's, so it is named here rather than in index.scss.
		transition: transform 150ms ease;

		&:hover {
			border-color: var(--border-hover);
			background: var(--main-hover);
			transform: translateY(-1px);
		}

		&:focus-visible {
			outline: 2px solid var(--link);
			outline-offset: 2px;
		}
	}

	.macro-card-head {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		min-width: 0;
	}

	.macro-card-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
		width: 1.75rem;
		height: 1.75rem;
		border-radius: 8px;
		background: var(--secondary);
		color: var(--main-text);
	}

	.macro-card-icon :global(.macro-card-spinner) {
		animation: spin 1s linear infinite;
	}

	.macro-card-name {
		// A name is free text of any length, and the grid's columns are fixed, so
		// it is clamped to two lines rather than allowed to stretch a card taller
		// than its neighbours. `break-word` covers a "name" with no spaces in it.
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
		overflow-wrap: break-word;
		font-weight: 500;
		line-height: 1.3;
	}

	.macro-card-meta {
		display: flex;
		flex-wrap: wrap;
		gap: 0.25rem 0.85rem;
	}

	.macro-card-stat {
		display: flex;
		align-items: center;
		gap: 0.35rem;
		font-size: 0.8rem;
		font-weight: 300;
		color: var(--tertiary-text);
	}

	.macro-card-modified {
		margin-top: auto;
		padding-top: 0.15rem;
	}

	.macro-hotkey {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		flex-wrap: wrap;
		padding: 0 0.25rem;
		font-size: 0.75rem;
		color: var(--tertiary-text);
	}

	.macro-hotkey :global(.macro-hotkey-icon) {
		flex-shrink: 0;
	}

	.macro-hotkey-combo {
		padding: 0.05rem 0.35rem;
		font-family: inherit;
		font-size: 0.72rem;
		color: var(--main-text);
		background: var(--secondary);
		border: 1px solid var(--border);
		border-radius: 5px;
	}
</style>
