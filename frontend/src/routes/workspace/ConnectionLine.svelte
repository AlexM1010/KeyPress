<script lang="ts">
	import { getBezierPath, useConnection } from '@xyflow/svelte';

	/**
	 * The dashed line that trails the cursor while the user drags out of a handle.
	 *
	 * Svelte Flow renders this itself, inside its own `<svg><g>` and only while a
	 * connection is in progress, and hands it no props at all - so the geometry
	 * has to be fetched rather than received, which is what `useConnection` is
	 * for. That is also why the markup below is a bare `<path>`: the `<svg>`
	 * wrapper, the viewport sizing and the valid/invalid class all belong to the
	 * library's own `ConnectionLine`, which this only fills in.
	 *
	 * The hook used to hand back a store, read as `$connection`. In 1.x it hands
	 * back `{ current }` - a rune-backed view of the same state - so this file
	 * reads a rune, and a Svelte file is runes or legacy but never both. Hence
	 * `$derived` below rather than the `$:` block this used to be; there are no
	 * props to convert alongside it.
	 */
	const connection = useConnection();

	/**
	 * `connection.current` is a discriminated union: `from` / `to` /
	 * `fromPosition` / `toPosition` exist only on the `inProgress: true` arm. It
	 * is therefore read once into a local so the narrowing survives, rather than
	 * re-read per field - a property access on the hook narrows for that
	 * expression only.
	 *
	 * `null` while idle, which the guard in the markup means nothing ever draws.
	 */
	const path = $derived.by(() => {
		const state = connection.current;
		if (!state.inProgress) return null;

		const [bezier] = getBezierPath({
			sourceX: state.from.x,
			sourceY: state.from.y,
			sourcePosition: state.fromPosition,
			targetX: state.to.x,
			targetY: state.to.y,
			targetPosition: state.toPosition
		});

		return bezier;
	});
</script>

{#if path}
	<path d={path} fill="none" stroke="#9ca3af" stroke-width="1" stroke-dasharray="5,5" />
{/if}
