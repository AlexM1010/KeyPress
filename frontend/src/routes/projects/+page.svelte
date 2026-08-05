<script lang="ts">
	// Browses the macros the user has actually saved on disk. The listing comes
	// from the Go runtime, which only exists once the app is running, so it is
	// fetched on mount rather than at build time.
	import { onMount } from 'svelte';
	import { base } from '$app/paths';
	import { FileWarning, Inbox, Loader } from 'lucide-svelte';

	import { ListProjects } from '$lib/wailsjs/go/main/App';
	import type { main } from '$lib/wailsjs/go/models';

	import TabTitle from '$lib/components/Projects/TabTitle.svelte';
	import Card from '$lib/components/Projects/Card/Card.svelte';
	import CardTitle from '$lib/components/Projects/Card/CardTitle.svelte';
	import CardDivider from '$lib/components/Projects/Card/CardDivider.svelte';
	import '$lib/index.scss';

	type LoadState =
		| { status: 'loading' }
		| { status: 'loaded'; projects: main.ProjectSummary[] }
		| { status: 'error'; message: string };

	let state: LoadState = { status: 'loading' };

	const formatModified = (modifiedAt: string): string => {
		if (!modifiedAt) return 'Unknown date';
		const date = new Date(modifiedAt);
		return Number.isNaN(date.getTime()) ? 'Unknown date' : date.toLocaleString();
	};

	const plural = (count: number, word: string): string =>
		`${count} ${word}${count === 1 ? '' : 's'}`;

	onMount(async () => {
		try {
			state = { status: 'loaded', projects: await ListProjects() };
		} catch (error) {
			state = {
				status: 'error',
				message: error instanceof Error ? error.message : String(error)
			};
		}
	});
</script>

<TabTitle title="Macros" />

<div class="macros-page">
	<h1 class="font-[var(--title-f)] text-2xl">Saved macros</h1>
	<p class="text-[var(--tertiary-text)] font-300">
		Every macro you save from the workspace gets its own file. Open one to preview its node graph.
	</p>

	{#if state.status === 'loading'}
		<div class="macros-placeholder">
			<Loader class="w-8 h-8" style="animation: spin 1s linear infinite" />
			<p class="font-300">Loading macros...</p>
		</div>
	{:else if state.status === 'error'}
		<div class="macros-placeholder text-[var(--accent-text)]">
			<FileWarning class="w-8 h-8" />
			<p class="font-300">Could not read your saved macros.</p>
			<p class="font-300 text-sm">{state.message}</p>
		</div>
	{:else if state.projects.length === 0}
		<div class="macros-placeholder text-[var(--tertiary-text)]">
			<Inbox class="w-8 h-8" />
			<p class="font-300">No saved macros yet.</p>
			<p class="font-300 text-sm">
				Build a flow in the workspace, give it a name and hit save.
			</p>
		</div>
	{:else}
		<div class="macros-list">
			{#each state.projects as project (project.id)}
				<Card href={`${base}/projects/${project.id}`} classes={['macro-card']}>
					<CardTitle title={project.name} />
					<CardDivider />
					<p class="font-300 text-[var(--tertiary-text)] text-sm">
						{plural(project.nodeCount, 'node')} &middot; {plural(project.edgeCount, 'connection')}
					</p>
					<p class="font-300 text-[var(--tertiary-text)] text-sm">
						Modified {formatModified(project.modifiedAt)}
					</p>
				</Card>
			{/each}
		</div>
	{/if}
</div>

<style lang="scss">
	.macros-page {
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
		padding: 5rem 1.5rem 2.5rem;
		color: var(--main-text);
	}

	.macros-placeholder {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.75rem;
		padding: 3rem 1rem;
		text-align: center;
	}

	.macros-list {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
		gap: var(--grid-gap, clamp(10px, 3vh, 25px));
		width: 100%;
		margin-top: 0.5rem;

		& > :global(.macro-card) {
			min-width: 280px;
			max-width: 400px;
			width: 100%;
			height: 100%;
			justify-self: left;
		}
	}
</style>
